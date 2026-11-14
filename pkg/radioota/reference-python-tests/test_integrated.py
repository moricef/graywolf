import unittest
import base64
import json
import sys
import tempfile
from pathlib import Path
from unittest.mock import Mock,patch
from send_integrated import *

class WireTests(unittest.TestCase):
    key=bytes(range(32))
    source=b'F4MLV-2'
    target=b'F4MLV-15'
    nonce=bytes(range(8))

    def reply(self,payload):
        return make_frame(self.key,self.target,self.source,self.nonce,ACK,204,payload)
    def decode(self,raw):
        return decode_reply(self.key,self.source,self.target,raw)
    @patch('send_integrated.time.sleep')
    @patch('send_integrated.open_serial')
    def test_full_igate_bridge_speed_negotiation(self,open_port,sleep):
        device=open_port.return_value
        device.readline.side_effect=[b'',b'OTABRIDGE OK\n']
        self.assertIs(open_igate_bridge('/dev/test'),device)
        device.write.assert_called_with(b'OTABRIDGE 921600\n')
        self.assertEqual(device.baudrate,921600)
        device.reset_input_buffer.assert_called()
        sleep.assert_called_once_with(1)
    def test_close_stops_integrated_bridge(self):
        client=Client.__new__(Client);client.serial=Mock();client.serial.readline.return_value=b'BRIDGE OFF OK\n'
        client.negotiated=False;client.fast=False
        client.close()
        client.serial.write.assert_called_once_with(b'BRIDGE OFF\n')
        client.serial.close.assert_called_once()
    def test_network_status(self):
        p=bytes([NETWORK_STATUS,0])+struct.pack('>I',0)+bytes([0,0,0])
        r=self.decode(self.reply(p));self.assertEqual(r['wifi_mode'],0)
        self.assertFalse(r['connected']);self.assertFalse(r['active_interface'])
        self.assertIsNone(self.decode(self.reply(p[:-1])))
        self.assertIsNone(self.decode(self.reply(p[:-3]+bytes([4,0,0]))))
    def test_power_cut_diagnostic_status(self):
        p=bytes([24,0])+struct.pack('>I',8192)+b'\1'+struct.pack('>IH',8192,2048)
        r=self.decode(self.reply(p));self.assertTrue(r['power_cut_paused'])
        self.assertEqual((r['written'],r['partial_offset'],r['partial_bytes']),(8192,8192,2048))
        self.assertIsNone(self.decode(self.reply(p[:-1])))
        self.assertIsNone(self.decode(self.reply(p+b'x')))
        self.assertIsNone(self.decode(self.reply(p[:6]+b'\2'+p[7:])))
    def test_explicit_busy_status_is_accepted_only_when_requested(self):
        payload=bytes([WINDOW_QUERY,5])+struct.pack('>II',8192,0)
        client=Client.__new__(Client);client.event=Mock();client.fast=False
        client.key,client.source,client.target,client.nonce=self.key,self.source,self.target,self.nonce
        client.route=None;client.default_timeout=1;client.serial=Mock()
        client.serial.readline.return_value=b'RX '+self.reply(payload).hex().encode()
        self.assertEqual(client.request(WINDOW_QUERY,204,expected=(0,5))['status'],5)
        with self.assertRaisesRegex(RuntimeError,'Unexpected response'):
            client.request(WINDOW_QUERY,204)
    def test_window_bitmap_reply(self):
        for op in (WINDOW_START,WINDOW_QUERY):
            p=bytes([op,0])+struct.pack('>II',4096,0x80000001)
            r=self.decode(self.reply(p));self.assertEqual(r['bitmap'],0x80000001)
            self.assertEqual(r['written'],4096)
            self.assertIsNone(self.decode(self.reply(p[:-1])))
            self.assertIsNone(self.decode(self.reply(p+b'x')))
    def test_route_transport(self):
        transport=RouteTransport(bytes(range(32)))
        raw=make_frame(self.key,self.source,self.target,self.nonce,DATA,0,bytes(175))
        wire=transport.encode(raw,phase=3);self.assertEqual(len(wire),255)
        self.assertEqual(transport.decode(wire),raw)
        self.assertIsNone(transport.decode(wire,phase=1))
        self.assertIsNone(transport.decode(wire[:-1]))
        self.assertIsNone(transport.decode(wire[:-1]+bytes([wire[-1]^1])))
        self.assertIsNone(RouteTransport(bytes(32)).decode(wire))
        with self.assertRaises(ValueError):transport.encode(raw+b'x')
    def test_maximum_data_frame(self):
        f=make_frame(self.key,self.source,self.target,self.nonce,DATA,0,bytes(CHUNK))
        self.assertEqual(len(f),255)
        with self.assertRaises(ValueError):make_frame(self.key,self.source,self.target,self.nonce,DATA,0,bytes(CHUNK+1))
    def test_short_reply_and_authentication(self):
        f=self.reply(bytes([DATA,0])+struct.pack('>I',408))
        self.assertEqual(self.decode(f)['written'],408)
        self.assertIsNone(self.decode(f[:-1]))
        self.assertIsNone(self.decode(f[:-1]+bytes([f[-1]^1])))
        self.assertIsNone(decode_reply(bytes(32),self.source,self.target,f))
        self.assertIsNone(decode_reply(self.key,b'F4MLV-3',self.target,f))
        self.assertIsNone(decode_reply(self.key,self.source,b'F4MLV-14',f))
    @patch('send_integrated.transfer_stop_and_wait')
    @patch('send_integrated.Client')
    def test_no_activate_stops_after_verification(self,client_class,transfer):
        image=b'\xe9'+bytes(63);client=client_class.return_value
        client.hello.return_value={'pending':False,'running':65536,'boot':65536}
        transfer.return_value={'written':len(image)}
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);image_path=root/'firmware.bin';key_path=root/'key';log_path=root/'run.jsonl';profiles_path=root/'profiles.json'
            image_path.write_bytes(image)
            key_path.write_text(base64.urlsafe_b64encode(self.key).decode().rstrip('='))
            profiles_path.write_text(json.dumps({
                'normal':{'frequency_hz':433775000,'bandwidth_hz':125000,'spreading_factor':12,'coding_rate':5,'power_dbm':20},
                'transfer':{'frequency_hz':434500000,'bandwidth_hz':500000,'spreading_factor':7,'coding_rate':5,'power_dbm':9}}))
            argv=['send_integrated.py','--board-id','0x54474732','--bridge','dummy','--key-file',str(key_path),
                  '--image',str(image_path),'--log',str(log_path),'--no-activate',
                  '--negotiate-profile','--radio-profiles',str(profiles_path)]
            with patch.object(sys,'argv',argv):self.assertEqual(main(),0)
            events=[json.loads(line)['event'] for line in log_path.read_text().splitlines()]
        self.assertIn('verified_not_activated',events)
        self.assertNotIn('activation_starting',events)
        client.request.assert_not_called()
        client.return_normal.assert_called_once()
        client.close.assert_called_once()
    @patch('send_integrated.transfer_stop_and_wait')
    @patch('send_integrated.Client')
    def test_replace_existing_discards_candidate_and_reopens_session(self,client_class,transfer):
        image=b'\xe9'+bytes(63);client=client_class.return_value
        initial={'pending':False,'running':65536,'boot':65536,'written':0}
        client.hello.side_effect=[initial,dict(initial)]
        transfer.return_value={'written':len(image)}
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);image_path=root/'firmware.bin';key_path=root/'key';log_path=root/'run.jsonl'
            image_path.write_bytes(image)
            key_path.write_text(base64.urlsafe_b64encode(self.key).decode().rstrip('='))
            argv=['send_integrated.py','--board-id','0x54474732','--bridge','dummy','--key-file',str(key_path),
                  '--image',str(image_path),'--log',str(log_path),'--replace-existing','--no-activate']
            with patch.object(sys,'argv',argv):self.assertEqual(main(),0)
            events=[json.loads(line)['event'] for line in log_path.read_text().splitlines()]
        client.request.assert_called_once_with(ABORT)
        self.assertEqual(client.hello.call_count,2)
        self.assertIn('previous_candidate_discarded',events)
    @patch('send_integrated.time.sleep')
    @patch('send_integrated.transfer_stop_and_wait')
    @patch('send_integrated.Client')
    def test_confirmation_timeout_rolls_back_reactivates_and_confirms(self,client_class,transfer,sleep):
        image=b'\xe9'+bytes(63);sha=hashlib.sha256(image).hexdigest();client=client_class.return_value
        old={'pending':False,'running':65536,'boot':65536,'written':0,'version':'4.0.2RXT-OTA','image_sha':'0'*64}
        new={'pending':True,'running':2031616,'boot':2031616,'written':0,'version':'4.0.2RXT-OTA-B','image_sha':sha}
        confirmed=dict(new,pending=False)
        client.hello.side_effect=[old,new,old,new,confirmed]
        transfer.return_value={'written':len(image)}
        client.request.side_effect=lambda op,*args,**kwargs: {'pending':False} if op==CONFIRM else {}
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);image_path=root/'firmware.bin';key_path=root/'key';log_path=root/'run.jsonl'
            image_path.write_bytes(image)
            key_path.write_text(base64.urlsafe_b64encode(self.key).decode().rstrip('='))
            argv=['send_integrated.py','--board-id','0x54474732','--bridge','dummy','--key-file',str(key_path),
                  '--image',str(image_path),'--log',str(log_path),'--exercise-confirm-timeout']
            with patch.object(sys,'argv',argv):self.assertIsNone(main())
            events=[json.loads(line)['event'] for line in log_path.read_text().splitlines()]
        operations=[call.args[0] for call in client.request.call_args_list]
        self.assertEqual(operations,[ACTIVATE,REACTIVATE,CONFIRM,CONFIRM,ABORT,ABORT])
        self.assertEqual(events.count('boot_pending_confirmation'),2)
        self.assertLess(events.index('confirmation_timeout_rollback'),events.index('radio_confirmed'))
        self.assertGreater(events.index('post_install_verified'),events.index('radio_confirmed'))
    def test_metadata_and_malformed_authenticated_replies(self):
        short=bytes([STATUS,0])+struct.pack('>I',408)
        version=b'4.0.2RXT-OTA-B'
        payload=short+b'\1'+struct.pack('>II',0x1f0000,0x1f0000)+bytes([len(version)])+version+bytes(range(32))
        r=self.decode(self.reply(payload))
        self.assertTrue(r['pending']);self.assertEqual(r['version'],version.decode())
        self.assertEqual(r['image_sha'],bytes(range(32)).hex())
        for truncated in (payload[:7],payload[:15],payload[:-1]):
            self.assertIsNone(self.decode(self.reply(truncated)))
        bad=bytearray(payload);bad[6]=2
        self.assertIsNone(self.decode(self.reply(bad)))
        bad=bytearray(payload);bad[15]=41
        self.assertIsNone(self.decode(self.reply(bad)))
        bad=bytearray(payload);bad[16]=255
        self.assertIsNone(self.decode(self.reply(bad)))

if __name__=='__main__':unittest.main()
