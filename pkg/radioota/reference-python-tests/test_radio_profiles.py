import unittest
from pathlib import Path
from unittest.mock import Mock,patch
from radio_profiles import RadioProfile,load_profiles
from send_integrated import Client,PREPARE_FAST,RETURN_NORMAL,STATUS

class ConfigurableProfileTests(unittest.TestCase):
    def profiles(self):return load_profiles(Path(__file__).with_name('radio-profiles.example.json'))
    def client(self):
        c=Client.__new__(Client);c.route=None;c.block_size=128;c.radio_profiles=self.profiles();c.normal_timeout=35;c.windowed=True
        c.event=Mock();c.request=Mock();c.bridge_profile=Mock();return c
    def test_explicit_profile_wire_and_json_validation(self):
        p=self.profiles()['transfer'];self.assertEqual(len(p.encode()),11)
        self.assertEqual(p.encode().hex(),'19e6b4f00003d090080603')
        self.assertEqual(p.bridge_command(1),b'PROFILEDEF 1 434550000 250000 8 6 3\n')
        for key,value in [('frequency_hz',True),('spreading_factor',8.0),('bandwidth_hz',12500)]:
            values=p.mapping();values[key]=value
            with self.assertRaises(ValueError):RadioProfile.from_mapping(values)
        values=p.mapping();values['typo']=1
        with self.assertRaises(ValueError):RadioProfile.from_mapping(values)
    @patch('send_integrated.time.sleep')
    def test_profile_is_sent_in_authenticated_request_then_activated(self,sleep):
        c=self.client();c.enter_fast()
        call=c.request.call_args_list[0];self.assertEqual(call.args[:2],(PREPARE_FAST,0))
        self.assertEqual(call.args[2],b'\2\0\x3c'+self.profiles()['transfer'].encode())
        c.bridge_profile.assert_called_once_with(True)
    @patch('send_integrated.time.sleep')
    def test_missing_profile_ack_leaves_controller_on_normal(self,sleep):
        c=self.client();c.request.side_effect=TimeoutError()
        with self.assertRaisesRegex(TimeoutError,'not acknowledged'):c.enter_fast()
        c.bridge_profile.assert_not_called();sleep.assert_called_once_with(65)
    def test_slow_page_burst_is_rejected_before_any_radio_request(self):
        c=self.client();c.route=object();c.radio_profiles['transfer']=c.radio_profiles['normal']
        with self.assertRaisesRegex(ValueError,'too slow for a page burst'):c.enter_fast()
        c.request.assert_not_called()
    @patch('send_integrated.time.sleep')
    def test_missing_return_ack_waits_for_lease_and_opens_new_session(self,sleep):
        c=self.client();c.profile_lease=60;c.hello=Mock()
        c.request.side_effect=[TimeoutError(),{'status':0}]
        c.return_normal()
        self.assertEqual(c.request.call_args_list[0].args,(RETURN_NORMAL,))
        self.assertEqual(c.request.call_args_list[1].args,(STATUS,))
        c.bridge_profile.assert_called_once_with(False)
        sleep.assert_called_once_with(65)
        c.hello.assert_called_once_with(attempts=3)
        c.event.assert_any_call('waiting_for_profile_recovery',seconds=65)

if __name__=='__main__':unittest.main()
