#!/usr/bin/env python3
"""Record actual SQLite/SCS HTTP component tests, including a local TLS flow."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess

parser=argparse.ArgumentParser()
parser.add_argument('--receipt',required=True)
args=parser.parse_args()
root=Path(__file__).resolve().parents[1]
files=sorted([*root.glob('cmd/**/*.go'),*root.glob('internal/**/*.go'),*root.glob('internal/**/*.sql'),*root.glob('pkg/**/*.go'),root/'go.mod',root/'go.sum',root/'api/openapi.json',Path(__file__).resolve()])
def hashes():return {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
before=hashes()
run=subprocess.run(['go','test','-race','-count=1','-json','./internal/websession','./internal/oidcattempt','./internal/oidcprovider','./internal/simpleauth','./internal/useradmin','./internal/storage'],cwd=root,env=dict(os.environ,GOTOOLCHAIN='go1.27.3'),capture_output=True,text=True,timeout=120)
events=[json.loads(line) for line in run.stdout.splitlines() if line.startswith('{')]
if run.returncode or any(e['Action'] in ['fail','skip'] for e in events):raise RuntimeError(run.stdout+run.stderr)
passed=[e['Test'] for e in events if e['Action']=='pass' and 'Test' in e]
required={'TestGuestLifecycleIsolationRenewalAndEnd','TestRealTLSGuestHTTPFlow','TestRevocationPreventsStaleConcurrentSessionCommit','TestGuestEndRevokesDocumentAccessAcrossStores','TestConcurrentGuestCreationOneBrowserOnePrincipal','TestCommitFailureDoesNotSendSuccessPayload','TestStaleBootstrapDoesNotOverwriteNewSessionCookie','TestAttemptBoundToActiveSessionAndConsumedOnce','TestTwoConnectionsConsumeOnlyOnce','TestUncertainCommitNeverReturnsProtocolMaterial','TestProviderTLSExchangeAndIdentityBinding','TestProviderRejectsInvalidTokensAndResponses','TestProviderRejectsUnsafeDiscovery','TestIdentityIssuerSeparationAndPostExchangeRevocation','TestConcurrentIdentityMappingCreatesOnePrincipal','TestProviderCancellationDuringTokenExchange','TestOIDCBrowserTLSLoginReplayAndLogout','TestOIDCBrowserWrongBindingAndProviderError','TestOIDCStartExpiryAndCallbackInput','TestLogoutCancelsGrantBeforeSessionCommit','TestOldCookieLogoutRevokesCommittedLoginSuccessor','TestOneSourceCookieCanGrantOnlyOneLogin','TestIdempotentRequestResumesOnlyOriginalPendingMaterial'}
required |= {'TestSimpleHTTPLoginIsolationResetAndLogout','TestSimpleHTTPRejectsCredentialOrLogoutRacesAtSessionCommit','TestSimpleHTTPStrictInputModeAndAttemptLimits'}
required |= {'TestLocalCredentialsCreateVerifyResetAndDisable','TestCredentialResetOrDisableDuringVerificationIsRejected','TestCredentialConcurrencyAndUncertainCommit','TestPasswordWorkBoundCancellationAndStorageFailure','TestCompiledUserCommandAndStoredCredential','TestLocalCredentialMigrationPreservesExistingIdentityAndSessions','TestLocalCredentialMigrationFailureRollsBackExistingData'}
assert required.issubset(passed) and before==hashes()
receipt={'timestamp_utc':datetime.now(timezone.utc).isoformat(),'passed':True,'tests':passed,'source_sha256':before,'scope':'SCS/SQLite HTTP session and guest components, local TLS cookie flow, exact Origin/host, independent guest principals, creation idempotency, end/cleanup intent, document access revocation and stale concurrent commit refusal and one-time browser-bound OIDC state/nonce/PKCE storage, actual local TLS discovery/code/PKCE/JWKS/token checks and issuer-sub identity mapping. Browser HTTP start/callback/login Cookie/logout are joined against the local TLS IdP. Also local Argon2id credential verification/reset/disable and bounded work, SQLite v3-to-v4 preservation/rollback and the compiled user-administration command with password stdin. Simple HTTP login/session/reset fencing, old-source logout, uncertain commit, strict credential input, global login rate bounds and cross-user authority are also covered. No product frontend, live IdP, cleanup worker, deployment resource/rate-limit acceptance or production deployment.'}
Path(args.receipt).write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps({'passed':True,'tests':len(passed)}))
