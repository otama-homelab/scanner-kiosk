import subprocess,os,time,signal,json,hashlib
from pathlib import Path
root=Path('/tmp/scanner-fairscan-jvm.k0rsMW');src=root/'FairScan-2c5230b12f69f3cee2cc5a970013da20848d1272';out=Path('/home/yamato/otama-homelab/scanner-kiosk/docs/design/research/correction-fairscan-jvm-resume-v060')
sourcehash={str(p.relative_to(src)):hashlib.sha256(p.read_bytes()).hexdigest() for p in src.rglob('*') if p.is_file()}
(root/'source-before.json').write_text(json.dumps(sourcehash,indent=2))
env=os.environ.copy();env.update(JAVA_HOME=str(root/'jdk-21.0.13+9'),GRADLE_USER_HOME=str(root/'gradle-home'),TMPDIR=str(root/'tmp'),JAVA_TOOL_OPTIONS='-XX:ActiveProcessorCount=2 -Xmx768m -XX:MaxMetaspaceSize=384m',GRADLE_OPTS='-Dorg.gradle.daemon=false -Dorg.gradle.workers.max=2 -Dkotlin.compiler.execution.strategy=in-process')
(root/'tmp').mkdir(exist_ok=True)
cmd=['taskset','-c',','.join(map(str,sorted(os.sched_getaffinity(0))[:2])),str(root/'gradle-9.8.2/bin/gradle'),'--no-daemon','-Dorg.gradle.jvmargs=-Xmx768m -XX:MaxMetaspaceSize=384m -XX:ActiveProcessorCount=2','--max-workers=2','--configure-on-demand','-I',str(root/'evaluate.init.gradle'),':evaluation:runOfficialQuadEvaluation','--console=plain','--stacktrace']
# ZIP extraction does not preserve executable bits.
(root/'gradle-9.8.2/bin/gradle').chmod(0o700)
start=time.monotonic();peak=0;reason=None
with (out/'evaluation.log').open('w') as log:
 p=subprocess.Popen(cmd,cwd=src,env=env,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 while p.poll() is None:
  total=0;largest=0
  for q in root.rglob('*'):
   if q.is_file():
    size=q.stat().st_size;total+=size;largest=max(largest,size)
  rss=0
  for proc in Path('/proc').iterdir():
   if proc.name.isdigit():
    try:
     stat=(proc/'stat').read_text().split()
     if int(stat[5])==p.pid:rss+=int(stat[23])*os.sysconf('SC_PAGE_SIZE')
    except (OSError,ValueError,IndexError):pass
  peak=max(peak,rss)
  if time.monotonic()-start>180:reason='build_timeout_180s'
  elif total>3*1024**3:reason='setup_tree_cap_3GiB'
  elif sum(q.stat().st_size for q in (src/'evaluation/reports').rglob('*') if q.is_file())>256*1024**2:reason='evaluation_output_cap_256MiB'
  elif any(q.stat().st_size>256*1024**2 for q in (root/'gradle-home/.tmp').glob('gradle_download*') if q.is_file()):reason='individual_download_cap_256MiB'
  elif rss>2*1024**3:reason='process_tree_rss_cap_2GiB'
  if reason:os.killpg(p.pid,signal.SIGKILL);break
  time.sleep(0.5)
 rc=p.wait()
unchanged=all((src/name).is_file() and hashlib.sha256((src/name).read_bytes()).hexdigest()==h for name,h in sourcehash.items())
result={'command':cmd,'cwd':str(src),'exit_code':rc,'stop_reason':reason,'elapsed_seconds':time.monotonic()-start,'peak_process_tree_rss_bytes':peak,'temp_tree_bytes':sum(q.stat().st_size for q in root.rglob('*') if q.is_file()),'source_files':len(sourcehash),'original_source_bytes_unchanged':unchanged,'evaluation_executed':True,'java_home':env['JAVA_HOME'],'java_tool_options':env['JAVA_TOOL_OPTIONS']}
(out/'evaluation.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
