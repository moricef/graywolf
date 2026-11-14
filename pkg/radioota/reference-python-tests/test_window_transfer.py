import unittest
import struct
from unittest.mock import Mock
from window_transfer import transfer,START,DATA,QUERY,FINISH,PAGE


class WindowClientTests(unittest.TestCase):
    def test_resume_skips_durable_page_and_resends_only_one_hole(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[
            {'written':PAGE},
            {'written':PAGE,'bitmap':0xffffffff^(1<<9)},
            {'written':2*PAGE,'bitmap':0xffffffff},
            {'written':2*PAGE,'running':0x10000,'boot':0x10000},
        ]
        image=bytes(2*PAGE);events=[]
        transfer(client,image,bytes(32),lambda kind,**kw:events.append((kind,kw)))
        offsets=[call.args[1] for call in client.send_unacknowledged.call_args_list]
        self.assertEqual(offsets,list(range(PAGE,2*PAGE,128))+[PAGE+9*128])
        self.assertEqual([call.args[0] for call in client.request.call_args_list],[START,QUERY,QUERY,FINISH])
        self.assertEqual(next(kw['missing'] for kind,kw in events if kind=='repair_page'),1<<9)

    def test_complete_checkpoint_needs_no_data(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':73},{'written':73}]
        transfer(client,bytes(73),bytes(32),lambda *a,**kw:None)
        client.send_unacknowledged.assert_not_called()

    def test_final_short_page_rejects_bitmap_outside_image(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':0},{'written':0,'bitmap':2}]
        with self.assertRaisesRegex(RuntimeError,'invalid page bitmap'):
            transfer(client,bytes(73),bytes(32),lambda *a,**kw:None)

    def test_invalid_resume_offset_is_rejected(self):
        client=Mock(retries=0,board_id=0x54474732);client.request.return_value={'written':128}
        with self.assertRaisesRegex(RuntimeError,'invalid durable'):
            transfer(client,bytes(PAGE),bytes(32),lambda *a,**kw:None)

    def test_lost_reply_recovers_session_without_resending_committed_page(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':0},TimeoutError('relay lost'),
                                    {'written':PAGE},{'written':PAGE}]
        events=[]
        transfer(client,bytes(PAGE),bytes(32),lambda kind,**kw:events.append((kind,kw)))
        client.recover_fast_session.assert_called_once_with()
        self.assertEqual(client.send_unacknowledged.call_count,32)
        self.assertIn(('link_recovered',{'written':PAGE}),events)

    def test_progress_regression_after_recovery_is_rejected(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':PAGE},TimeoutError('relay lost'),{'written':0}]
        with self.assertRaisesRegex(RuntimeError,'unexpected durable offset'):
            transfer(client,bytes(2*PAGE),bytes(32),lambda *a,**kw:None)

    def test_relay_restart_loses_cache_but_keeps_durable_pages(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':0},
            {'written':PAGE,'bitmap':0xffffffff},
            {'written':2*PAGE,'bitmap':0xffffffff},
            TimeoutError('relay restarted'),{'written':2*PAGE},
            {'written':3*PAGE,'bitmap':0xffffffff},{'written':3*PAGE}]
        events=[]
        transfer(client,bytes(3*PAGE),bytes(32),lambda kind,**kw:events.append((kind,kw)),
                 exercise_relay_restart=True)
        client.recover_fast_session.assert_called_once_with()
        calls=client.send_unacknowledged.call_args_list
        resets=[i for i,c in enumerate(calls) if c.args[0]==21]
        self.assertEqual(resets,[80]) # two full pages and half of the third
        data=[c.args[1] for c in calls if c.args[0]==DATA]
        self.assertEqual(data,list(range(0,3*PAGE,128))+list(range(2*PAGE,3*PAGE,128)))
        self.assertIn(('relay_restart_recovered',{'written':2*PAGE}),events)

    def test_relay_restart_must_actually_interrupt_the_link(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':0}]+[
            {'written':i*PAGE,'bitmap':0xffffffff} for i in range(1,4)]
        with self.assertRaisesRegex(RuntimeError,'did not exercise link recovery'):
            transfer(client,bytes(3*PAGE),bytes(32),lambda *a,**kw:None,
                     exercise_relay_restart=True)
        self.assertNotIn(FINISH,[c.args[0] for c in client.request.call_args_list])

    def test_relay_restart_refuses_existing_checkpoint(self):
        client=Mock(retries=0,board_id=0x54474732);client.request.return_value={'written':PAGE}
        with self.assertRaisesRegex(RuntimeError,'fresh transfer'):
            transfer(client,bytes(3*PAGE),bytes(32),lambda *a,**kw:None,
                     exercise_relay_restart=True)
        client.send_unacknowledged.assert_not_called()

    def test_burst_and_lost_commit_ack_resume_at_different_offsets(self):
        for point,resumed,mode in [('burst',2*PAGE,1),('ack',3*PAGE,2)]:
            with self.subTest(point=point):
                client=Mock(retries=0,board_id=0x54474732)
                client.request.side_effect=[{'written':0},
                    {'written':PAGE,'bitmap':0xffffffff},
                    {'written':2*PAGE,'bitmap':0xffffffff},
                    TimeoutError('injected restart'),{'written':resumed}]+[
                    {'written':pos,'bitmap':0xffffffff} for pos in range(resumed+PAGE,5*PAGE,PAGE)
                    ]+[{'written':4*PAGE}]
                events=[]
                transfer(client,bytes(4*PAGE),bytes(32),lambda kind,**kw:events.append((kind,kw)),
                         exercise_relay_restart=True,relay_restart_point=point)
                calls=client.send_unacknowledged.call_args_list
                armed=[c.args for c in calls if c.args[0]==22]
                self.assertEqual(armed,[(22,2*PAGE,bytes([mode]))])
                data=[c.args[1] for c in calls if c.args[0]==DATA]
                self.assertEqual(data,list(range(0,3*PAGE,128))+list(range(resumed,4*PAGE,128)))
                self.assertIn(('relay_restart_recovered',{'written':resumed}),events)

    def test_lost_commit_ack_must_recover_committed_page(self):
        client=Mock(retries=0,board_id=0x54474732)
        client.request.side_effect=[{'written':0},
            {'written':PAGE,'bitmap':0xffffffff},{'written':2*PAGE,'bitmap':0xffffffff},
            TimeoutError('relay restarted'),{'written':2*PAGE}]
        with self.assertRaisesRegex(RuntimeError,'unexpected durable offset'):
            transfer(client,bytes(4*PAGE),bytes(32),lambda *a,**kw:None,
                     exercise_relay_restart=True,relay_restart_point='ack')


if __name__=='__main__':unittest.main()


class BoardManifestTests(unittest.TestCase):
    def test_each_board_id_reaches_window_manifest(self):
        for board in (0x54474732, 0x484C5633, 0x51524C50):
            with self.subTest(board=board):
                client=Mock(retries=0,board_id=board)
                client.request.side_effect=[{'written':73},{'written':73}]
                transfer(client,bytes(73),bytes(32),lambda *a,**kw:None)
                manifest=client.request.call_args_list[0].args[2]
                self.assertEqual(struct.unpack('>I',manifest[36:40])[0],board)
