"""Graywolf integration checks: mocks only, no serial devices opened."""
import base64
import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch
import send
import send_integrated as engine


class WorkerIntegrationTests(unittest.TestCase):
    def test_serial_lines_and_exclusivity_set_before_open(self):
        device = Mock()
        def opened():
            self.assertFalse(device.rts)
            self.assertFalse(device.dtr)
            self.assertTrue(device.exclusive)
            self.assertEqual(device.port, '/dev/test')
        device.open.side_effect = opened
        with patch.object(send.serial, 'Serial', return_value=device), patch.object(send.os, 'name', 'posix'):
            self.assertIs(send.open_serial('/dev/test'), device)

    def test_bridge_timeout_closes_port(self):
        with patch.object(engine, 'open_serial') as opened, patch.object(engine.time, 'monotonic', side_effect=[0, 16]):
            with self.assertRaises(TimeoutError):
                engine.open_igate_bridge('/dev/test')
            opened.return_value.close.assert_called_once()

    def test_bridge_io_error_closes_port(self):
        with patch.object(engine, 'open_serial') as opened:
            opened.return_value.reset_input_buffer.side_effect = OSError('unplugged')
            with self.assertRaises(OSError):
                engine.open_igate_bridge('/dev/test')
            opened.return_value.close.assert_called_once()

    def test_counter_never_goes_backwards_after_clock_change(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(engine.time, 'time', return_value=100):
            client = engine.Client.__new__(engine.Client)
            client.counter_file = Path(folder) / 'counter'
            client.counter_file.write_text('200\n')
            client.request = Mock(return_value={'nonce':bytes(8)})
            client.event = Mock(); client.negotiated = False
            client.hello(); client.hello()
            self.assertEqual(client.counter_file.read_text().strip(), '202')
            self.assertEqual([c.args[1] for c in client.request.call_args_list], [201, 202])

    def test_normal_update_and_final_readback_rejections(self):
        image = b'\xe9' + bytes(63)
        sha = hashlib.sha256(image).hexdigest()
        old = dict(pending=False, running=0x10000, boot=0x10000, version='old', image_sha='0'*64)
        new = dict(pending=True, running=0x1f0000, boot=0x1f0000, version='new', image_sha=sha)
        for problem in [None, 'pending', 'version', 'boot', 'image_sha']:
            with self.subTest(problem=problem), tempfile.TemporaryDirectory() as folder:
                root = Path(folder)
                (root/'image').write_bytes(image)
                (root/'key').write_text(base64.urlsafe_b64encode(bytes(range(32))).decode().rstrip('='))
                final = dict(new, pending=False)
                if problem:
                    final[problem] = {'pending':True, 'version':'wrong', 'boot':0x10000, 'image_sha':'0'*64}[problem]
                with patch.object(engine, 'Client') as cls, patch.object(engine, 'transfer_stop_and_wait', return_value={'written':len(image)}), patch.object(engine.time, 'sleep'), patch.object(sys, 'argv', ['send_integrated.py','--board-id','0x54474732','--bridge','dummy','--key-file',str(root/'key'),'--image',str(root/'image'),'--log',str(root/'log')]):
                    c = cls.return_value; c.hello.side_effect = [old, new, final]
                    c.request.side_effect = lambda op,*a,**kw: dict(new,pending=False) if op==engine.CONFIRM else {}
                    if problem:
                        with self.assertRaises(AssertionError): engine.main()
                    else: engine.main()
                    events = [json.loads(l)['event'] for l in (root/'log').read_text().splitlines()]
                    self.assertEqual('post_install_verified' in events, problem is None)
                    self.assertNotIn('confirmation_withheld', events)
                    self.assertNotIn('previous_candidate_discarded', events)
                    c.close.assert_called_once()
