#!/usr/bin/env python3
"""Fail closed on incomplete, stale or mismatched release-candidate evidence."""
import pathlib
import re
import subprocess

from evidence_binding import (AUTH_INPUTS, EVIDENCE_INPUTS, EvidenceError, candidate,
                              read_json, validate_artifact, validate_frontend,
                              validate_go, validate_packaged, validate_source_report)

root = pathlib.Path(__file__).resolve().parents[1]
REQUIRED_REPORTS = ('go-coverage', 'frontend-coverage', 'artifact-security', 'integration',
                    'fuzz', 'mutation', 'stress', 'soak', 'desktop-e2e', 'public-testnets',
                    'independent-review', 'packaged-recovery', 'packaged-send', 'release-auth', 'release-evidence')
PACKAGED_SEND_INPUTS = ('scripts/packaged-send.py', 'tests/e2e/local_geth.py', 'tests/e2e/x11.py',
                        'tests/oracles/evm/geth-genesis.json')
PACKAGED_SEND_STEPS = (
    'Pinned actual geth executes fresh blocks with public genesis-only funds',
    'Fresh explicit testnet profile restores the public BIP39 fixture through masked Electron input',
    'Electron configures and approves the actual loopback RPC; funded native balance is visible',
    'Ordinary Electron form prepares the exact decimal payment through the real Go/node pipeline',
    'Electron Send prepares, presents the exact Go-authored plan and reauthenticates in its masked dialog',
    'Independent actual geth receipt, recipient delta, source fee debit and delayed nonce prove exactly one executed payment',
    'Disposable app files and captured logs contain no tested password/phrase canaries')


def validate_packaged_send(base, report, current):
    validate_packaged(report, current)
    validate_source_report(base, report, PACKAGED_SEND_INPUTS)
    if report.get('steps') != [{'name': name, 'status': 'PASS'} for name in PACKAGED_SEND_STEPS]:
        raise EvidenceError('packaged-send-seven-required-steps-incomplete')
    isolation = report.get('networkIsolation')
    if not isinstance(isolation, dict) or type(isolation.get('uid')) is not int or isolation['uid'] <= 0 or isolation.get('interfaces') != ['lo'] or isolation.get('externalRoutes') is not False or not isinstance(isolation.get('networkNamespace'), str) or not re.fullmatch(r'net:\[[0-9]+\]', isolation['networkNamespace']):
        raise EvidenceError('packaged-send-local-isolation-incomplete')
    if report.get('publicNetworkTests') != 'NOT_RUN' or report.get('mainnetSpending') != 'NOT_APPLICABLE: no outside network route' or not isinstance(report.get('osMatrix'), str) or not report['osMatrix'].startswith('NOT_RUN:'):
        raise EvidenceError('packaged-send-local-scope-mismatch')
    node = report.get('node')
    if not isinstance(node, dict) or type(node.get('chainId')) is not int or node['chainId'] != 11155111 or node.get('image') != 'ethereum/client-go:v1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109' or node.get('binarySHA256') != '75da01386ed074c086f18e52e034b79cfc6974232f196ca1c72bb16ca169e3e2':
        raise EvidenceError('packaged-send-real-node-identity-mismatch')
    transfer = report.get('transfer')
    if not isinstance(transfer, dict):
        raise EvidenceError('packaged-send-execution-evidence-missing')
    def atomic(name):
        value = transfer.get(name)
        if not isinstance(value, str) or not re.fullmatch(r'(?:0|[1-9][0-9]{0,77})', value):
            raise EvidenceError('packaged-send-invalid-atomic-evidence')
        return int(value)
    amount = atomic('atomicAmount')
    if amount != 12345678912345678 or transfer.get('source') != '0x9858EfFD232B4033E47d90003D41EC34EcaEda94' or transfer.get('recipient') != '0x1111111111111111111111111111111111111111' or type(transfer.get('chainId')) is not int or transfer['chainId'] != 11155111 or type(transfer.get('sourcePendingNonceAfter')) is not int or transfer['sourcePendingNonceAfter'] != 1:
        raise EvidenceError('packaged-send-independent-transfer-mismatch')
    approval = report.get('approval')
    if not isinstance(approval, dict) or set(approval) != {'networkVisible', 'atomicAmountVisible', 'atomicAmount'} or approval.get('networkVisible') is not True or approval.get('atomicAmountVisible') is not True or approval.get('atomicAmount') != str(amount):
        raise EvidenceError('packaged-send-approval-evidence-incomplete')
    identity = transfer.get('identity')
    receipt = transfer.get('receipt')
    if not isinstance(identity, str) or not re.fullmatch(r'0x[0-9a-f]{64}', identity) or not isinstance(receipt, dict) or receipt.get('transactionHash') != identity or receipt.get('status') != '0x1' or receipt.get('type') != '0x2':
        raise EvidenceError('packaged-send-successful-receipt-missing')
    if not isinstance(receipt.get('from'), str) or receipt['from'].lower() != transfer['source'].lower() or not isinstance(receipt.get('to'), str) or receipt['to'].lower() != transfer['recipient'].lower():
        raise EvidenceError('packaged-send-receipt-parties-mismatch')
    for name in ('gasUsed', 'effectiveGasPrice'):
        if not isinstance(receipt.get(name), str) or not re.fullmatch(r'0x[0-9a-f]{1,64}', receipt[name]):
            raise EvidenceError('packaged-send-fee-evidence-invalid')
    fee = int(receipt['gasUsed'], 16) * int(receipt['effectiveGasPrice'], 16)
    if int(receipt['gasUsed'], 16) != 21000 or int(receipt['effectiveGasPrice'], 16) <= 0:
        raise EvidenceError('packaged-send-native-execution-fee-invalid')
    if atomic('sourceBefore') != 1000 * 10 ** 18 or atomic('nativeRecipientBefore') != 0 or atomic('nativeRecipientAfter') - atomic('nativeRecipientBefore') != amount or atomic('actualFee') != fee or atomic('sourceBefore') - atomic('sourceAfter') != amount + fee:
        raise EvidenceError('packaged-send-independent-balance-or-fee-mismatch')


def verify(base=root, check_version=True):
    base = pathlib.Path(base)
    failed = []

    def check(label, operation):
        try:
            return operation()
        except EvidenceError as error:
            failed.append(label + ': ' + str(error))
        except (OSError, ValueError, TypeError, KeyError, AttributeError, subprocess.SubprocessError):
            # A malformed local report must not echo arbitrary report content,
            # subprocess output, secret fragments or an exception traceback.
            failed.append(label + ': missing-or-invalid-evidence')
        return None

    def traceability():
        requirements = read_json(base, 'docs/traceability.json').get('requirements')
        if not isinstance(requirements, list) or not requirements:
            raise EvidenceError('requirements-missing')
        for row in requirements:
            if not isinstance(row, dict) or not isinstance(row.get('id'), str) or not re.fullmatch(r'[A-Za-z0-9_.:-]{1,160}', row['id']):
                raise EvidenceError('invalid-requirement-row')
            if row.get('status') != 'PASS':
                failed.append('requirement ' + row['id'] + ': incomplete')
    check('traceability', traceability)

    reports = {}
    for name in REQUIRED_REPORTS:
        report = check(name, lambda name=name: read_json(base, 'reports/' + name + '.json'))
        if report is not None:
            reports[name] = report
            if report.get('status') != 'PASS':
                failed.append(name + ': missing-or-incomplete')
    coverage = check('coverage', lambda: read_json(base, 'reports/coverage.json'))
    if coverage is not None:
        packages = coverage.get('packages')
        if not isinstance(packages, list) or not packages or any(not isinstance(row, dict) or row.get('status') != 'PASS' for row in packages):
            failed.append('coverage: package-floors-incomplete')
    if 'go-coverage' in reports and coverage is not None:
        check('go-coverage-binding', lambda: validate_go(base, reports['go-coverage'], coverage))
    if 'frontend-coverage' in reports:
        check('frontend-coverage-binding', lambda: validate_frontend(base, reports['frontend-coverage']))
    for name, inputs in (('release-auth', AUTH_INPUTS), ('release-evidence', EVIDENCE_INPUTS)):
        if name in reports:
            check(name + '-binding', lambda name=name, inputs=inputs: validate_source_report(base, reports[name], inputs))
    current = check('candidate', lambda: candidate(base))
    if current is not None:
        if 'artifact-security' in reports:
            check('artifact-security-binding', lambda: validate_artifact(base, reports['artifact-security'], current))
        if 'packaged-recovery' in reports:
            check('packaged-recovery-binding', lambda: validate_packaged(reports['packaged-recovery'], current))
        if 'packaged-send' in reports:
            check('packaged-send-binding', lambda: validate_packaged_send(base, reports['packaged-send'], current))
    if check_version:
        def version():
            # Keep the pre-existing archive, runtime and exact-version check;
            # its potentially untrusted stdout is never inserted into errors.
            result = subprocess.run([str(base / 'scripts/with-toolchain.sh'), 'python3', 'scripts/verify-version.py'],
                                    cwd=base, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
            if result.returncode:
                raise EvidenceError('artifact-version-integrity-failed')
        check('artifact-version', version)
    return failed


def main():
    failed = verify()
    print('FAIL: production release blocked' if failed else 'PASS: required candidate-bound evidence present')
    for line in failed:
        print(' - ' + line)
    return bool(failed)


if __name__ == '__main__':
    raise SystemExit(main())
