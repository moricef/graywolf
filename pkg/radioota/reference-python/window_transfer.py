"""Page bursts, bitmap repair, and authenticated restart from durable progress."""
import struct
import time
import zlib

START,DATA,QUERY,FINISH,ACTIVATE=range(15,20)
PAGE,CHUNK=4096,128


def deflate_pages(image):
    """Compressed container read by page_inflater.h: for each 4096-byte page,
    a big-endian 16-bit length then the raw deflate of that page."""
    out=bytearray()
    for i in range(0,len(image),PAGE):
        c=zlib.compressobj(9,zlib.DEFLATED,-15,9)
        data=c.compress(image[i:i+PAGE])+c.flush()
        if not 0<len(data)<=PAGE+64:raise ValueError('page does not fit a container record')
        out+=struct.pack('>H',len(data))+data
    return bytes(out)


def transfer(client,image,sha,event,fault_checks=False,exercise_resume=False,exercise_relay_restart=False,
             relay_restart_point='cache',block_size=128,verify_sample=False,compress=False):
    maximum=175 if getattr(client,'route',None) else 204
    if type(block_size) is not int or not 32<=block_size<=maximum:raise ValueError('invalid block size for this topology')
    if block_size!=128 and (fault_checks or exercise_resume or exercise_relay_restart):
        raise ValueError('legacy fault injections require 128-byte blocks')
    chunk=block_size
    bitmap_bytes=4*((((PAGE+chunk-1)//chunk)+31)//32)
    if relay_restart_point not in ('cache','burst','ack'):raise ValueError('invalid relay restart point')
    if exercise_relay_restart and len(image)<3*PAGE:
        raise ValueError('relay restart test requires at least three pages')
    image_size=len(image)
    if compress:
        if fault_checks or exercise_resume or exercise_relay_restart:
            raise ValueError('compressed transfer does not support the legacy fault injections')
        # The window protocol carries the container; sha and image_size describe the decoded image.
        image=deflate_pages(image)
        event('compressed',image_bytes=image_size,container_bytes=len(image),ratio=round(len(image)/image_size,4))
    manifest=struct.pack('>I',len(image))+sha+struct.pack('>I',client.board_id)
    if chunk!=128 or compress:manifest+=struct.pack('>H',chunk)
    if compress:manifest+=bytes([1])+struct.pack('>I',image_size)
    initial=client.request(START,0,manifest,timeout=12)
    if chunk!=128 and initial.get('bitmap_bytes')!=bitmap_bytes:
        raise RuntimeError('receiver did not negotiate the requested bitmap width')
    position=initial['written']
    if exercise_relay_restart and position!=0:
        raise RuntimeError('relay restart test requires a fresh transfer')
    if position>len(image) or position!=len(image) and position%PAGE:
        raise RuntimeError('invalid durable receiver offset')
    event('durable_progress',written=position,total=len(image))
    if fault_checks and position==0:
        # Diagnostics only: one loss on each link, both reported separately.
        client.request(20,0,struct.pack('>I',1<<5))
        event('inject_downstream_loss',block=5)
    start_offset=position
    started=time.monotonic()
    resume_exercised=False
    repairs=0
    recoveries=0
    recovery_seconds=0
    relay_restart_injected=False
    relay_restart_recovered=False
    while position<len(image):
        length=min(PAGE,len(image)-position)
        mask=(1<<((length+chunk-1)//chunk))-1
        missing=mask
        try:
            if exercise_relay_restart and relay_restart_point!='cache' and not relay_restart_injected and position==2*PAGE:
                client.send_unacknowledged(22,position,bytes([1 if relay_restart_point=='burst' else 2]))
                relay_restart_injected=True
                event('relay_restart_armed',point=relay_restart_point,durable=position)
            for attempt in range(16):
                for index in range((length+chunk-1)//chunk):
                    if not missing&(1<<index):continue
                    if fault_checks and position==0 and attempt==0 and index==7:
                        event('inject_upstream_loss',block=index)
                        continue
                    offset=position+index*chunk
                    client.send_unacknowledged(DATA,offset,image[offset:min(offset+chunk,position+length)])
                    if exercise_relay_restart and relay_restart_point=='cache' and not relay_restart_injected and position==2*PAGE and index==15:
                        # The first half-page is in relay RAM, not yet forwarded.
                        client.send_unacknowledged(21,0,b'')
                        relay_restart_injected=True
                        event('relay_restart_requested',durable=position,cached_bytes=16*CHUNK)
                reply=client.request(QUERY,position,mask.to_bytes(bitmap_bytes,'big'),timeout=getattr(client,'window_timeout',20),attempts=2)
                bitmap=reply['bitmap']
                if bitmap&~mask or reply['written']>len(image):raise RuntimeError('invalid page bitmap')
                if bitmap==mask and reply['written']==position+length:break
                missing=mask&~bitmap
                if not missing:raise RuntimeError('complete bitmap without durable commit')
                repairs+=missing.bit_count()
                event('repair_page',offset=position,missing=missing,attempt=attempt+1)
            else:raise RuntimeError('page repair attempts exhausted')
        except TimeoutError:
            recoveries+=1
            if recoveries>5:raise
            event('link_lost',durable=position,recovery=recoveries)
            recovery_started=time.monotonic()
            client.recover_fast_session()
            restored=client.request(START,0,manifest,timeout=12)
            resumed=restored['written']
            if relay_restart_injected and not relay_restart_recovered:
                expected=(3 if relay_restart_point=='ack' else 2)*PAGE
                if resumed!=expected:raise RuntimeError('unexpected durable offset after relay restart')
                relay_restart_recovered=True
                event('relay_restart_recovered',written=resumed)
            # A compressed receiver resumes at the page holding its last whole record.
            earliest=position-(PAGE if compress else 0)
            if resumed<max(0,earliest) or resumed>position+length or resumed!=len(image) and resumed%PAGE:
                raise RuntimeError('unexpected durable offset after link recovery')
            position=resumed
            recovery_seconds+=time.monotonic()-recovery_started
            event('link_recovered',written=position)
            continue
        position+=length
        event('page_committed',written=position,total=len(image))
        if exercise_resume and not resume_exercised and position>=2*PAGE and position+PAGE<len(image):
            # Flush half a page through the relay, then reboot the receiver.
            # The resume offset must exclude this uncommitted RAM buffer.
            partial_mask=0xffff
            for attempt in range(4):
                for index in range(16):
                    if partial_mask&(1<<index):
                        offset=position+index*chunk
                        client.send_unacknowledged(DATA,offset,image[offset:min(offset+chunk,position+length)])
                partial=client.request(QUERY,position,struct.pack('>I',0xffffffff),timeout=20)
                if partial['written']!=position or partial['bitmap']&~0xffff:
                    raise RuntimeError('unexpected progress before restart')
                partial_mask=0xffff&~partial['bitmap']
                if not partial_mask:break
            else:raise RuntimeError('partial page was not staged before restart')
            client.request(13)  # authenticated diagnostic reset, after the ACK
            event('receiver_restart_requested',durable=position,volatile_bytes=16*CHUNK)
            recovery_started=time.monotonic()
            client.recover_fast_session()
            restored=client.request(START,0,manifest,timeout=12)
            if restored['written']!=position:raise RuntimeError('durable progress lost after restart')
            recovery_seconds+=time.monotonic()-recovery_started
            event('persistent_resume_confirmed',written=position)
            resume_exercised=True
    if exercise_relay_restart and not relay_restart_recovered:
        raise RuntimeError('relay restart did not exercise link recovery')
    result=client.request(26 if verify_sample else FINISH,len(image),timeout=12)
    if result['written']!=len(image):raise RuntimeError('incomplete finished image')
    event('verified',elapsed_s=time.monotonic()-started,recovery_s=recovery_seconds,start_offset=start_offset,
          repairs=repairs,retries=client.retries,reply={k:(v.hex() if isinstance(v,bytes) else v) for k,v in result.items()})
    return result
