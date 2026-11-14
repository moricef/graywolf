#!/usr/bin/env python3
"""OTA v2: authenticated radio arm, image transfer and radio boot confirmation.

The target's USB port is neither opened nor required by this program.
"""
import argparse
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import struct
import time
from send import open_serial
from route_transport import RouteTransport
from radio_profiles import load_profiles

PREFIX=b'\x3c\xff\x7e\x02'
DOMAIN=b'LORA-APRS-OTA2'
START,DATA,FINISH,ACTIVATE,ABORT,HELLO,CONFIRM,STATUS,TEST_FAIL_ONCE,REACTIVATE=range(1,11)
PREPARE_FAST,RETURN_NORMAL=11,12
NETWORK_STATUS=14
WINDOW_START,WINDOW_DATA,WINDOW_QUERY,WINDOW_FINISH,WINDOW_ACTIVATE=range(15,20)
ACK=128
CHUNK=204
BOARD=0x54474732

def make_frame(key,source,target,nonce,op,offset,payload=b''):
    if len(source)>8 or len(target)>8 or len(payload)>CHUNK:raise ValueError('size')
    raw=PREFIX+bytes([op])+nonce+struct.pack('>IH',offset,len(payload))+source.ljust(8,b'\0')+target.ljust(8,b'\0')+payload
    return raw+hmac.digest(key,DOMAIN+raw,'sha256')[:16]

def decode_reply(key,source,target,raw):
    if len(raw)<57 or raw[:5]!=PREFIX+bytes([ACK]):return None
    offset,n=struct.unpack('>IH',raw[13:19])
    if len(raw)!=51+n or raw[19:27]!=target.ljust(8,b'\0') or raw[27:35]!=source.ljust(8,b'\0'):return None
    if not hmac.compare_digest(raw[-16:],hmac.digest(key,DOMAIN+raw[:-16],'sha256')[:16]):return None
    p=raw[35:-16]
    result=dict(nonce=raw[5:13],offset=offset,op=p[0],status=p[1],written=struct.unpack('>I',p[2:6])[0])
    if p[0] in (WINDOW_START,WINDOW_QUERY):
        if len(p) not in (10,14,18,22):return None
        result['bitmap']=int.from_bytes(p[6:],'big');result['bitmap_bytes']=len(p)-6;return result
    if p[0]==25:
        if len(p) not in (30,62):return None
        boot,frames,errors,wire_bytes,air_us=struct.unpack('>IIIIQ',p[6:30])
        result.update(stats_boot=boot,tx_frames=frames,tx_errors=errors,tx_bytes=wire_bytes,tx_airtime_us=air_us)
        if len(p)==62:result['elf_sha256']=p[30:62].hex()
        return result
    if p[0]==24:
        if len(p)!=13 or p[6]>1:return None
        result.update(power_cut_paused=bool(p[6]),partial_offset=int.from_bytes(p[7:11],'big'),
                      partial_bytes=int.from_bytes(p[11:13],'big'));return result
    if p[0]==NETWORK_STATUS:
        if len(p)!=9 or p[6]>3 or p[7]>1 or p[8]>1:return None
        result.update(wifi_mode=p[6],connected=bool(p[7]),active_interface=bool(p[8]));return result
    if len(p)>6:
        if len(p)<48 or p[6] not in (0,1):return None
        vl=p[15]
        if vl>40 or len(p)!=48+vl:return None
        try:version=p[16:16+vl].decode('ascii')
        except UnicodeDecodeError:return None
        result.update(pending=bool(p[6]),running=struct.unpack('>I',p[7:11])[0],boot=struct.unpack('>I',p[11:15])[0],
                      version=version,image_sha=p[16+vl:].hex())
    return result

class OperatorPause(Exception):
    """Operator requested a pause before activation; retain durable pages."""

def open_igate_bridge(port,baud=921600):
    """Open a full iGate firmware console and switch it to bridge speed."""
    device=open_serial(port)
    try:
        device.reset_input_buffer()
        deadline=time.monotonic()+15;next_send=0
        while time.monotonic()<deadline:
            if time.monotonic()>=next_send:
                device.write(b'OTABRIDGE %d\n'%baud);device.flush();next_send=time.monotonic()+1
            if device.readline().strip()==b'OTABRIDGE OK':
                device.baudrate=baud;time.sleep(1);device.reset_input_buffer();return device
        raise TimeoutError('iGate OTA bridge did not start')
    except BaseException:
        device.close()
        raise

class Client:
    def __init__(self,port,key,source,target,counter_file,event,serial_device=None):
        if serial_device is None:
            self.serial=open_igate_bridge(port)
        else:
            self.serial=serial_device
        self.key,self.source,self.target=key,source,target
        self.counter_file=counter_file;self.nonce=bytes(8);self.event=event;self.retries=0
        self.default_timeout=1;self.negotiated=False;self.fast=False
        self.route=None;self.chunk=CHUNK;self.normal_timeout=12
        self.radio_profiles=None;self.block_size=128;self.profile_lease=30;self.windowed=False;self.window_timeout=20
    def set_radio_profiles(self,profiles):
        self.radio_profiles=profiles
        for index,name in enumerate(('normal','transfer')):
            command=profiles[name].bridge_command(index);expected=b'PROFILEDEF '+str(index).encode()+b' OK'
            for attempt in range(4):
                self.serial.write(command);deadline=time.monotonic()+1
                while time.monotonic()<deadline:
                    line=self.serial.readline().strip()
                    if line==expected:break
                    if line.startswith(b'PROFILEDEF ') and line.endswith(b'ERROR'):
                        raise ValueError('profile rejected by controller radio: '+name)
                else:continue
                break
            else:raise TimeoutError('bridge does not support configurable profiles')
        self.normal_timeout=max(35 if self.route else 12,4*profiles['normal'].airtime()+5)
        fast=profiles['transfer']
        burst=((4096+self.block_size-1)//self.block_size)*(fast.airtime(80+self.block_size)+.02)
        self.window_timeout=max(20,(4*burst if self.route else 0)+4*fast.airtime()+5)
        self.event('radio_profiles_configured',**{name:p.mapping() for name,p in profiles.items()})
    def check_pause(self):
        path=getattr(self,'pause_file',None)
        if path and not getattr(self,'activation_started',False) and not getattr(self,'closing',False) and path.exists():
            raise OperatorPause()
    def request(self,op,offset=0,payload=b'',expected=0,drop_ack=False,timeout=None,attempts=16):
        self.check_pause()
        if timeout is None:timeout=self.default_timeout
        raw=make_frame(self.key,self.source,self.target,self.nonce if op!=HELLO else bytes(8),op,offset,payload)
        if self.route:raw=self.route.encode(raw)
        for attempt in range(attempts):
            self.check_pause()
            self.serial.write(b'TX '+raw.hex().encode()+b'\n')
            self.event('rf_tx',op=op,offset=offset,wire_bytes=len(raw),attempt=attempt+1,fast=self.fast)
            deadline=time.monotonic()+timeout
            while time.monotonic()<deadline:
                self.check_pause()
                line=self.serial.readline().strip()
                if line.startswith(b'TXDONE '):
                    ok=line==b'TXDONE 0'
                    self.event('rf_tx_done',op=op,offset=offset,wire_bytes=len(raw),attempt=attempt+1,fast=self.fast,ok=ok)
                    if not ok:raise RuntimeError('controller radio transmission failed')
                    continue
                if not line.startswith(b'RX '):continue
                try:
                    wire=bytes.fromhex(line[3:].decode())
                    if self.route:
                        wire=self.route.decode(wire)
                        if wire is None:continue
                    r=decode_reply(self.key,self.source,self.target,wire)
                except (ValueError,IndexError,UnicodeDecodeError):continue
                if r is None or r['op']!=op or r['offset']!=offset:continue
                if op!=HELLO and r['nonce']!=self.nonce:continue
                if drop_ack and attempt==0:
                    self.event('drop_ack',op=op,offset=offset);break
                allowed=(expected,) if isinstance(expected,int) else expected
                if r['status'] not in allowed:
                    self.event('protocol_rejected',op=op,status=r['status'],offset=offset,payload_bytes=len(payload))
                    raise RuntimeError(f'Unexpected response: {r}')
                if op==HELLO:self.nonce=r['nonce']
                return r
            self.retries+=1;self.event('retry',op=op,offset=offset,attempt=attempt+1)
        raise TimeoutError(f'op={op} offset={offset}')
    def hello(self,attempts=80):
        previous=int(self.counter_file.read_text()) if self.counter_file.exists() else 0
        counter=max(int(time.time()),previous+1)
        if counter>0xffffffff:raise ValueError('counter exhausted')
        self.counter_file.write_text(str(counter)+'\n')
        response=self.request(HELLO,counter,attempts=attempts,timeout=self.normal_timeout if self.negotiated else 3)
        self.event('hello',**printable(response));return response

    def recover_fast_session(self):
        # No final RF command is assumed to reach a failed relay or target.
        # Both have a 30-second inactivity lease in this client configuration.
        self.bridge_profile(False)
        self.event('waiting_for_profile_recovery',seconds=self.profile_lease+5)
        time.sleep(self.profile_lease+5)
        self.hello(attempts=3)
        self.enter_fast(self.profile_lease)

    def send_unacknowledged(self,op,offset,payload):
        self.check_pause()
        raw=make_frame(self.key,self.source,self.target,self.nonce,op,offset,payload)
        if self.route:raw=self.route.encode(raw)
        self.serial.write(b'TX '+raw.hex().encode()+b'\n')
        self.event('rf_tx',op=op,offset=offset,wire_bytes=len(raw),attempt=1,fast=self.fast)
        allowance=max(5,self.radio_profiles['transfer' if self.fast else 'normal'].airtime()+2) if self.radio_profiles else 5
        deadline=time.monotonic()+allowance
        while time.monotonic()<deadline:
            line=self.serial.readline().strip()
            if line==b'TXDONE 0':
                self.event('rf_tx_done',op=op,offset=offset,wire_bytes=len(raw),attempt=1,fast=self.fast,ok=True)
                time.sleep(.005);return
            if line.startswith((b'TXDONE ',b'ERROR')):raise RuntimeError('bridge burst transmission failed')
        raise TimeoutError('bridge burst transmission')

    def bridge_profile(self,fast):
        command=b'PROFILE '+(b'1' if fast else b'0')+b'\n'
        deadline=time.monotonic()+3;next_send=0
        expected=b'PROFILE '+(b'1' if fast else b'0')+b' OK'
        while time.monotonic()<deadline:
            if time.monotonic()>=next_send:
                self.serial.write(command);next_send=time.monotonic()+.5
            if self.serial.readline().strip()==expected:
                self.fast=fast
                response_time=max(2,4*self.radio_profiles['transfer'].airtime()+2) if self.radio_profiles else (2 if self.route else 1)
                self.default_timeout=response_time if fast else self.normal_timeout;return
        raise TimeoutError('bridge profile command')
    def enable_profiles(self):
        self.negotiated=True;self.bridge_profile(False)
    def enter_fast(self,lease=None,drop_prepare_ack=False):
        if lease is None:lease=60 if self.radio_profiles else 30
        if not 15<=lease<=60:raise ValueError('profile lease outside 15..60 seconds')
        self.profile_lease=lease
        payload=bytes([1])+struct.pack('>H',lease)
        if self.radio_profiles:
            normal=self.radio_profiles['normal'];fast=self.radio_profiles['transfer']
            if 2*normal.airtime()+5>=lease:raise ValueError('normal profile is too slow for the profile lease')
            if fast.airtime()+5>=lease:raise ValueError('transfer frame is too slow for the profile lease')
            if self.windowed and self.route and ((4096+getattr(self,'block_size',128)-1)//getattr(self,'block_size',128))*(fast.airtime(80+getattr(self,'block_size',128))+.02)+5>=lease:
                raise ValueError('transfer profile is too slow for a page burst within the profile lease')
            payload=bytes([2])+struct.pack('>H',lease)+fast.encode()
        try:self.request(PREPARE_FAST,0,payload,timeout=self.normal_timeout,attempts=1,drop_ack=drop_prepare_ack)
        except TimeoutError:
            self.event('prepare_ack_missing')
            if self.radio_profiles:
                # An unsupported relay drops the request before forwarding it.
                # If only the ACK was lost, peers return through their own lease.
                self.event('waiting_for_profile_recovery',seconds=lease+5)
                time.sleep(lease+5)
                raise TimeoutError('custom RF profile was not acknowledged; controller stayed on normal profile')
        self.bridge_profile(True);time.sleep(2.2)
        self.request(STATUS,attempts=8)
        self.event('fast_profile_confirmed',lease_seconds=lease)
    def return_normal(self):
        missing=False
        try:self.request(RETURN_NORMAL,attempts=3)
        except TimeoutError:
            missing=True;self.event('return_ack_missing')
        self.bridge_profile(False)
        if missing:
            # The target may already be back on the normal profile while the
            # relay remains fast until its lease expires.  The old target
            # session expires at the same time, so recovery needs a new nonce.
            wait=self.profile_lease+5
            self.event('waiting_for_profile_recovery',seconds=wait)
            time.sleep(wait)
            self.hello(attempts=3)
        else:
            time.sleep(.8)
        self.request(STATUS,attempts=4)
        self.event('normal_profile_confirmed')
    def close(self):
        self.closing=True
        if self.negotiated and self.fast:
            try:self.return_normal()
            except Exception:
                # Receiver has its own lease even if the radio link has failed.
                self.bridge_profile(False)
        try:
            self.serial.write(b'BRIDGE OFF\n');self.serial.flush()
            deadline=time.monotonic()+2
            while time.monotonic()<deadline:
                if self.serial.readline().strip()==b'BRIDGE OFF OK':break
        except Exception:
            pass
        self.serial.close()

def printable(reply):return {k:(v.hex() if isinstance(v,bytes) else v) for k,v in reply.items()}

def main():
    ap=argparse.ArgumentParser(description=__doc__)
    ap.add_argument('--bridge',required=True);ap.add_argument('--key-file',required=True,type=Path)
    ap.add_argument('--source',default='F4MLV-2');ap.add_argument('--target',default='F4MLV-15')
    ap.add_argument('--image',type=Path,required=True);ap.add_argument('--log',type=Path,required=True)
    ap.add_argument('--board-id',type=lambda value:int(value,0),required=True,choices=(BOARD,0x484C5633,0x51524C50))
    ap.add_argument('--pause-file',type=Path,help='Operator interface: pause safely before activation when this file exists')
    ap.add_argument('--no-activate',action='store_true',help='Stop after whole-image verification and normal-profile recovery')
    ap.add_argument('--replace-existing',action='store_true',help='Discard a previous inactive candidate before starting this image')
    ap.add_argument('--counter-file',type=Path,help='Persistent counter for this controller/target')
    ap.add_argument('--resume-log',type=Path,help='Resume the same authenticated receiver session from a previous log')
    ap.add_argument('--exercise-rollback',action='store_true')
    ap.add_argument('--exercise-confirm-timeout',action='store_true',help='Withhold confirmation, observe rollback, then activate again')
    ap.add_argument('--fault-checks',action='store_true')
    ap.add_argument('--negotiate-profile',action='store_true',help='Start at the bench normal profile; negotiate the temporary fast profile')
    ap.add_argument('--radio-profiles',type=Path,help='JSON file with normal and transfer profiles; transfer settings are sent over authenticated RF')
    ap.add_argument('--via',help='Relay identity for the test log; requires a configured relay and target')
    ap.add_argument('--route-key-file',type=Path,help='Separate transport key for the controller, relay and target')
    ap.add_argument('--require-offline',action='store_true',help='Require radio-reported Wi-Fi off and no active IP interface before and after updating')
    ap.add_argument('--block-size',type=int,default=128,help='Window payload bytes, 32..204 direct or 32..175 routed; requires compatible peers')
    ap.add_argument('--windowed',action='store_true',help='Use page bursts, relay repair cache, and persistent page checkpoints')
    ap.add_argument('--compress',action='store_true',help='With --windowed: send each 4 KB page deflate-compressed; the receiver decodes before writing')
    ap.add_argument('--exercise-resume',action='store_true',help='Reboot after two pages plus a volatile half-page, then resume')
    ap.add_argument('--exercise-relay-restart',action='store_true',help='Restart diagnostic relay with half a page cached, then require link recovery')
    ap.add_argument('--relay-restart-point',choices=['cache','burst','ack'],default='cache',help='Inject before forwarding, during a page burst, or before forwarding a committed-page ACK')
    args=ap.parse_args();image=args.image.read_bytes();sha=hashlib.sha256(image).digest()
    if args.negotiate_profile and args.resume_log:raise ValueError('profile resume not supported by this client')
    if args.radio_profiles and not args.negotiate_profile:raise ValueError('--radio-profiles requires --negotiate-profile')
    profiles=load_profiles(args.radio_profiles) if args.radio_profiles else None
    if args.via and (not args.route_key_file or not args.negotiate_profile):raise ValueError('relay requires route key and profile negotiation')
    if args.compress and not args.windowed:raise ValueError('--compress requires --windowed')
    if args.windowed and args.resume_log:raise ValueError('window mode uses durable target progress, not --resume-log')
    if args.block_size!=128 and not args.windowed:raise ValueError('block-size requires window mode')
    if args.block_size!=128 and (args.fault_checks or args.exercise_resume or args.exercise_relay_restart):raise ValueError('legacy fault injections require 128-byte blocks')
    if not 32<=args.block_size<=(175 if args.via else 204):raise ValueError('block size outside frame capacity')
    if args.exercise_resume and not args.windowed:raise ValueError('resume test requires window mode')
    if args.exercise_relay_restart and not args.windowed:raise ValueError('relay restart test requires window mode')
    if args.relay_restart_point!='cache' and not args.exercise_relay_restart:raise ValueError('restart point requires --exercise-relay-restart')
    if args.exercise_relay_restart and args.exercise_resume:raise ValueError('exercise receiver and relay restarts separately')
    if args.no_activate and (args.exercise_rollback or args.exercise_confirm_timeout):raise ValueError('activation diagnostics require activation')
    key=base64.urlsafe_b64decode(args.key_file.read_text().strip()+'=')
    if len(key)!=32 or image[:1]!=b'\xe9':raise ValueError('key/image')
    args.log.parent.mkdir(parents=True,exist_ok=True)
    with args.log.open('w') as log:
        def event(kind,**data):
            line=json.dumps(dict(time=time.time(),event=kind,**data));log.write(line+'\n');log.flush()
            if kind!='block':print(line,flush=True)
        counter_file=args.counter_file or args.log.parent/(args.target+'.counter')
        c=Client(args.bridge,key,args.source.encode(),args.target.encode(),counter_file,event)
        c.board_id=args.board_id
        c.pause_file=args.pause_file
        try:
            if args.via:
                route_key=base64.urlsafe_b64decode(args.route_key_file.read_text().strip()+'=')
                c.route=RouteTransport(route_key,c.source,c.target,args.via.encode());c.chunk=175;c.normal_timeout=35
                event('route_selected',via=args.via,chunk=c.chunk)
            c.windowed=args.windowed;c.block_size=args.block_size
            if profiles:c.set_radio_profiles(profiles)
            if args.negotiate_profile:c.enable_profiles()
            if args.resume_log:
                previous=[json.loads(x) for x in args.resume_log.read_text().splitlines()]
                old_image=next(x for x in previous if x['event']=='image')
                if old_image['bytes']!=len(image) or old_image['sha256']!=sha.hex():raise ValueError('resume image mismatch')
                old_hello=next(x for x in reversed(previous) if x['event']=='hello')
                c.nonce=bytes.fromhex(old_hello['nonce'])
                before=c.request(STATUS)
                event('hello',**printable(before))
            else:before=c.hello()
            assert not before['pending']
            if args.replace_existing:
                c.request(ABORT)
                event('previous_candidate_discarded')
                before=c.hello()
                assert not before['pending'] and before['written']==0
            if args.require_offline:
                net=c.request(NETWORK_STATUS);assert net['wifi_mode']==0 and not net['connected'] and not net['active_interface']
                event('offline_before_update',reply=printable(net))
            if args.negotiate_profile:c.enter_fast()
            event('image',bytes=len(image),sha256=sha.hex(),board_id=args.board_id,initial=printable(before))
            manifest=struct.pack('>I',len(image))+sha+struct.pack('>I',args.board_id)
            if args.windowed:
                from window_transfer import transfer
                r=transfer(c,image,sha,event,args.fault_checks,args.exercise_resume,args.exercise_relay_restart,args.relay_restart_point,block_size=args.block_size,compress=args.compress)
                assert r['running']==before['running'] and r['boot']==before['boot']
            else:
                r=transfer_stop_and_wait(c,args,image,manifest,before,event)
            if args.negotiate_profile:c.return_normal()
            if args.no_activate:
                event('verified_not_activated',bytes=len(image),sha256=sha.hex())
                return 0
            c.check_pause()
            c.activation_started=True
            event('activation_starting')
            if args.exercise_rollback:c.request(TEST_FAIL_ONCE)
            # A windowed activation names the transported stream: the container when compressed.
            stream_bytes=r['written'] if args.windowed else len(image)
            try:c.request(WINDOW_ACTIVATE if args.windowed else ACTIVATE,stream_bytes,attempts=3,timeout=c.normal_timeout if c.negotiated else 3)
            except TimeoutError:event('activation_ack_missing')
            time.sleep(5)
            new=c.hello()
            if args.exercise_rollback:
                assert new['running']==before['running'] and new['version']==before['version'] and not new['pending']
                event('rollback_confirmed',reply=printable(new))
                try:c.request(REACTIVATE,0,sha,attempts=3,timeout=c.normal_timeout if c.negotiated else 3)
                except TimeoutError:event('reactivation_ack_missing')
                time.sleep(5)
                new=c.hello()
            assert new['pending'] and new['running']!=before['running'] and new['image_sha']==sha.hex()
            event('boot_pending_confirmation',reply=printable(new))
            if args.exercise_confirm_timeout:
                event('confirmation_withheld')
                for elapsed in range(0,190,5):
                    time.sleep(5)
                    if elapsed%30==25:print('Withholding confirmation:',elapsed+5,'seconds',flush=True)
                fallback=c.hello()
                assert not fallback['pending'] and fallback['running']==before['running'] and fallback['version']==before['version']
                event('confirmation_timeout_rollback',reply=printable(fallback))
                try:c.request(REACTIVATE,0,sha,attempts=3,timeout=c.normal_timeout if c.negotiated else 3)
                except TimeoutError:event('reactivation_ack_missing')
                time.sleep(5);new=c.hello()
                assert new['pending'] and new['running']!=before['running'] and new['image_sha']==sha.hex()
                event('boot_pending_confirmation',reply=printable(new))
            r=c.request(CONFIRM,0,sha);assert not r['pending']
            # A lost confirmation ACK must remain safely retryable.
            r=c.request(CONFIRM,0,sha);assert not r['pending']
            event('radio_confirmed',reply=printable(r))
            if args.require_offline:
                net=c.request(NETWORK_STATUS);assert net['wifi_mode']==0 and not net['connected'] and not net['active_interface']
                event('offline_after_update',reply=printable(net))
            c.request(ABORT)
            final=c.hello()
            assert not final['pending'] and final['running']==new['running'] and final['boot']==new['boot']
            assert final['version']==new['version'] and final['image_sha']==sha.hex()
            event('post_install_verified',reply=printable(final))
            c.request(ABORT)
        except OperatorPause:
            event('operator_paused')
            return 75
        finally:
            c.close()

def transfer_stop_and_wait(c,args,image,manifest,before,event):
    resume_offset=c.request(START,0,manifest,timeout=12)['written']
    assert resume_offset<=len(image) and (resume_offset==len(image) or resume_offset%c.chunk==0)
    if args.resume_log:event('resumed',offset=resume_offset,previous_log=str(args.resume_log))
    else:assert resume_offset==0
    if args.fault_checks and resume_offset==0:
        c.request(FINISH,len(image),expected=2)
        c.request(DATA,c.chunk,image[c.chunk:2*c.chunk],expected=2)
    started=time.monotonic()
    for offset in range(resume_offset,len(image),c.chunk):
        block=image[offset:offset+c.chunk]
        r=c.request(DATA,offset,block,drop_ack=args.fault_checks and offset==resume_offset)
        assert r['written']==offset+len(block)
        event('block',offset=offset,written=r['written'])
        if offset//c.chunk%500==0:print(f'{r["written"]}/{len(image)} bytes',flush=True)
    r=c.request(FINISH,len(image));assert r['running']==before['running'] and r['boot']==before['boot']
    event('verified',elapsed_s=time.monotonic()-started,start_offset=resume_offset,retries=c.retries,reply=printable(r))
    return r


if __name__=='__main__':raise SystemExit(main())
