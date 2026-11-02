#!/usr/bin/env python3
"""Typed denial controls for the real-node payment acceptance driver."""
import importlib.util
from pathlib import Path
import queue
import unittest
from unittest import mock

spec=importlib.util.spec_from_file_location('portable_send_control',Path(__file__).with_name('portable-send.py'))
send=importlib.util.module_from_spec(spec)
spec.loader.exec_module(send)


class PreviewDenialControls(unittest.TestCase):
    def client(self,**fields):
        client=send.Client.__new__(send.Client)
        client.counter,client.session=0,'public-test-session-identity'
        client.responses=queue.Queue()
        client.write=mock.Mock()
        response=dict(version=1,id='1',session=client.session,method='send',body=None,error='REQUEST_DENIED')
        response.update(fields)
        client.responses.put(response)
        return client

    def test_exact_typed_denial_without_payload_is_not_spend_authority(self):
        client=self.client()
        self.assertIsNone(client.call('send',{'id':'public-unrecorded-preview'},expected_error='REQUEST_DENIED'))
        self.assertEqual(client.write.call_count,1)

    def test_success_wrong_error_payload_and_foreign_response_are_distinct_failures(self):
        cases=[({'error':'','body':{'signed':'public-unexpected-payload'}},'gained spend authority'),
               ({'error':'REQUEST_FAILED_STATE_UNKNOWN'},'unexpected public error category'),
               ({'body':{'signed':'public-unexpected-payload'}},'response payload'),
               ({'id':'foreign-request'},'request/session binding')]
        for fields,reason in cases:
            with self.subTest(fields=fields),self.assertRaisesRegex(RuntimeError,reason):
                self.client(**fields).call('send',{'id':'public-unrecorded-preview'},expected_error='REQUEST_DENIED')

    def test_preview_must_fail_before_any_password_or_authorization_prompt(self):
        client=self.client(method='prompt',body={'id':'a'*32,'kind':'confirm','title':'Authorize exact transfer'})
        with self.assertRaisesRegex(RuntimeError,'unexpectedly opened an authorization prompt'):
            client.call('send',{'id':'public-unrecorded-preview'},expected_error='REQUEST_DENIED')
        self.assertEqual(client.write.call_count,1)


if __name__=='__main__':
    unittest.main(verbosity=2)
