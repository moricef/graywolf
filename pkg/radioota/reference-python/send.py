#!/usr/bin/env python3
"""Direct-link laboratory OTA. Receiver must be armed through its USB port."""
import argparse
import hashlib
import hmac
import json
import os
from pathlib import Path
import secrets
import struct
import time
import serial

PREFIX=b'\x3c\xff\x7e\x01'
START,DATA,FINISH,ACTIVATE,ABORT,ACK=1,2,3,4,5,128
BOARD=0x54474732

CHUNK=204

def frame(key,target,nonce,op,offset,payload=b'',source=b'F4MLV-2'):
    if len(payload)>CHUNK: raise ValueError('payload too long')
    head=PREFIX+bytes([op])+nonce+struct.pack('>IH',offset,len(payload))+source.ljust(8,b'\0')+target.ljust(8,b'\0')+payload
    context=bytes([len(target)])+target+head
    return head+hmac.digest(key,context,'sha256')[:16]

def decode(key,target,nonce,data):
    if len(data)<51 or len(data)>255 or data[:4]!=PREFIX or data[5:13]!=nonce: return None
    offset,n=struct.unpack('>IH',data[13:19])
    if len(data)!=51+n:return None
    tag=hmac.digest(key,bytes([len(target)])+target+data[:-16],'sha256')[:16]
    if not hmac.compare_digest(tag,data[-16:]):return None
    return data[4],offset,data[35:-16]

def open_serial(port):
    # Apply both inactive levels when opening the port. Opening first asserts
    # the modem lines long enough to reset CH343-connected ESP32 boards.
    device=serial.Serial(port=None,baudrate=115200,timeout=.1)
    if os.name == 'posix': device.exclusive=True
    device.rts=False;device.dtr=False;device.port=port;device.open()
    return device

def command(device,text,prefix,timeout=5):
    device.write(text.encode()+b'\n');deadline=time.monotonic()+timeout
    while time.monotonic()<deadline:
        line=device.readline().decode('ascii','replace').strip()
        if line.startswith(prefix):return line
    raise TimeoutError(prefix)

def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--bridge',required=True);ap.add_argument('--receiver',required=True)
    ap.add_argument('--image',type=Path,required=True);ap.add_argument('--target',default='F4MLV-15')
    ap.add_argument('--log',type=Path,required=True)
    ap.add_argument('--activate',action='store_true')
    ap.add_argument('--fault-checks',action='store_true',help='bad tag, out-of-order block, duplicate and lost ACK checks')
    args=ap.parse_args();image=args.image.read_bytes();target=args.target.encode('ascii')
    if not 1<=len(target)<=8:raise ValueError('target length')
    if image[:1]!=b'\xe9':raise ValueError('not an ESP32 application image')
    args.log.parent.mkdir(parents=True,exist_ok=True)
    key=secrets.token_bytes(32)
    secretfile=args.log.with_suffix('.key')
    with os.fdopen(os.open(secretfile,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600),'wb') as out:out.write(key)
    with open_serial(args.bridge) as tx,open_serial(args.receiver) as rx,args.log.open('w') as log:
        def event(kind,**fields):
            record=dict(time=time.time(),event=kind,**fields);log.write(json.dumps(record)+'\n');log.flush()
            if kind!='block':print(json.dumps(record),flush=True)
        before=command(rx,'INFO','INFO ');bridge=command(tx,'INFO','INFO ')
        armed=command(rx,'ARM '+args.target+' '+key.hex(),'ARMED ')
        nonce=bytes.fromhex(armed.split()[1]);event('start',before=before,bridge=bridge,size=len(image),sha256=hashlib.sha256(image).hexdigest(),nonce=nonce.hex())
        retries=0;faults=[]
        def exchange(op,offset,payload=b'',expected=0,drop_first=False,bad_tag=False,timeout=5):
            nonlocal retries
            packet=frame(key,target,nonce,op,offset,payload)
            if bad_tag:packet=packet[:-1]+bytes([packet[-1]^1])
            attempts=1 if bad_tag else 8
            for attempt in range(attempts):
                tx.write(b'TX '+packet.hex().encode()+b'\n')
                deadline=time.monotonic()+timeout
                while time.monotonic()<deadline:
                    line=tx.readline().strip()
                    if not line.startswith(b'RX '):continue
                    try:reply=decode(key,target,nonce,bytes.fromhex(line[3:].decode()))
                    except ValueError:continue
                    if reply is None:continue
                    code,seq,body=reply
                    if code!=ACK or seq!=offset or len(body)!=6 or body[0]!=op:continue
                    if drop_first and attempt==0:
                        event('drop_ack',op=op,offset=offset);break
                    status=body[1];written=struct.unpack('>I',body[2:])[0]
                    if status!=expected:raise RuntimeError(f'op={op} status={status}, expected {expected}, offset={written}')
                    if bad_tag:raise AssertionError('bad tag received authenticated reply')
                    return written
                if bad_tag:return None
                retries+=1;event('retry',op=op,offset=offset,attempt=attempt+1)
            raise TimeoutError(f'OTA op={op} offset={offset}')
        manifest=struct.pack('>I',len(image))+hashlib.sha256(image).digest()+struct.pack('>I',BOARD)
        if args.fault_checks:
            exchange(START,0,manifest,bad_tag=True,timeout=1)
            info=command(rx,'INFO','INFO ');assert 'state=0 offset=0' in info
            faults.append('bad_tag_rejected')
            wrong=struct.pack('>I',512)+bytes(32)+struct.pack('>I',BOARD)
            assert exchange(START,0,wrong)==0
            for offset in range(0,512,CHUNK):
                exchange(DATA,offset,image[offset:min(offset+CHUNK,512)])
            assert exchange(FINISH,512,expected=4)==0
            faults.append('wrong_sha256_rejected')
            assert exchange(START,0,manifest)==0
            assert exchange(DATA,0,image[:CHUNK])==CHUNK
            old_nonce=nonce
            reboot=command(rx,'RESET','BOOT ',timeout=15)
            info=command(rx,'INFO','INFO ')
            assert 'state=0 offset=0' in info and before.split('running=')[1].split()[0] == info.split('running=')[1].split()[0]
            armed=command(rx,'ARM '+args.target+' '+key.hex(),'ARMED ')
            nonce=bytes.fromhex(armed.split()[1]);assert nonce!=old_nonce
            faults.append('reset_during_transfer_keeps_old_app')
            event('interruption_check',boot=reboot,info=info)
        assert exchange(START,0,manifest)==0
        if args.fault_checks:
            assert exchange(FINISH,len(image),expected=2)==0
            assert exchange(DATA,CHUNK,image[CHUNK:2*CHUNK],expected=2)==0
            faults.extend(['incomplete_finish_rejected','out_of_order_rejected'])
        started=time.monotonic()
        for offset in range(0,len(image),CHUNK):
            block=image[offset:offset+CHUNK]
            written=exchange(DATA,offset,block,drop_first=args.fault_checks and offset==0)
            assert written==offset+len(block)
            if args.fault_checks and offset==0:
                assert exchange(DATA,offset,block)==written
                faults.extend(['lost_ack_retried','duplicate_not_written_twice'])
            event('block',offset=offset,written=written)
            if offset//CHUNK%100==0:print(f'{written}/{len(image)} bytes',flush=True)
        assert exchange(FINISH,len(image))==len(image)
        info=command(rx,'INFO','INFO ');assert 'state=2 ' in info
        event('verified',elapsed_s=time.monotonic()-started,retries=retries,checks=faults,info=info)
        if args.activate:
            exchange(ACTIVATE,len(image))
            deadline=time.monotonic()+15;boot=''
            while time.monotonic()<deadline:
                line=rx.readline().decode('ascii','replace').strip()
                if line.startswith('BOOT '):boot=line
                if boot and line.startswith('INFO '):
                    previous=before.split('running=')[1].split()[0]
                    current=line.split('running=')[1].split()[0]
                    selected=line.split('boot=')[1].split()[0]
                    assert current!=previous and current==selected, (before,line)
                    event('booted',boot=boot,info=line);break
            else:raise TimeoutError('no new firmware boot confirmation')
        else:
            # Leave the image verified but unselected; current application still boots.
            event('not_activated')

if __name__=='__main__':main()
