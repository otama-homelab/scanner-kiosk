#!/usr/bin/env python3
"""Prepare one official Docling CPU candidate; retain failures without fallback."""
import argparse
import email.parser
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import urllib.request
import urllib.parse
import zipfile
import html
import tempfile

def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify_ref(root, ref):
    # Same containment contract as OCR preparation; stream the 203MiB TableFormer weight.
    root = root.resolve(strict=True)
    relative = Path(ref["path"])
    if relative.is_absolute() or ".." in relative.parts:
        raise ValueError("evidence path outside root")
    path = (root / relative).resolve(strict=True)
    if not path.is_relative_to(root) or not path.is_file() or path.stat().st_size > 1 << 30:
        raise ValueError("invalid evidence file")
    if digest(path) != ref["sha256"]:
        raise ValueError("changed evidence identity")
    return path

CANDIDATE = "docling-cpu-2.149.0"
COMMIT = "8103d817983eeaaa2234d110072fa70d5c59a075"
TABLE_REV = "fc0f2d45e2218ea24bce5045f58a389aed16dc23"
LAYOUT_REV = "8f39ad3c0b4c58e9c2d2c84a38465abf757272d8"
PACKAGES = {"docling-slim": "2.149.0", "docling-core": "2.106.0", "docling-ibm-models": "4.0.4"}
CONFIG = {"device": "cpu", "threads": 2, "table_mode": "accurate", "ocr": "tesseract-5.5.4",
          "languages": ["jpn", "eng"], "pages_per_process": 1, "remote": False, "enrichment": False}
LIMITS = {"timeout_seconds": 900, "max_rss_bytes": 2 << 30, "max_temp_disk_bytes": 1 << 30}
MODELS = [
    ("docling-models", TABLE_REV, "model_artifacts/tableformer/accurate/tableformer_accurate.safetensors",
     "2a7d6c924b3cd12fb99a09280ca9c33a89c5d60b93253617d2e088c1a40374d9"),
    ("docling-models", TABLE_REV, "model_artifacts/tableformer/accurate/tm_config.json",
     "984e122ceb8ccf84d84c9d2882f6f2302a44b4f1e577babd6289892c36f3cffd"),
    ("docling-layout-heron", LAYOUT_REV, "model.safetensors",
     "00333a43451945aaf89db8ca9c0a17e75d1537c17db60fdb91aa95f4c7929e0c"),
    ("docling-layout-heron", LAYOUT_REV, "config.json",
     "fdea30805ce2f5666b147fca941dcdd27ad468e27d6ed21902207d3da056a97d"),
    ("docling-layout-heron", LAYOUT_REV, "preprocessor_config.json",
     "cd38cd59999e7a95d68e487fbe5132df3d4e5c32a0836add57e6126ba0c4eaf1"),
]


def write_json(path, value):
    with path.open("x", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2, allow_nan=False)
        stream.write("\n")
    path.chmod(0o600)


def failure_record(stage, reason, inputs, evidence):
    return {"format": "scanner-eval-structure-disposition", "version": 1,
            "candidate_id": CANDIDATE, "status": "unavailable", "stage": stage,
            "reason": reason, "inputs": inputs, "evidence": evidence, "fallback": False,
            "quality_measured": False, "resources_measured": False, "adoption": "defer",
            "source_commit": COMMIT, "packages": PACKAGES, "models": MODELS,
            "configuration": CONFIG, "limits": LIMITS,
            "package_legitimacy_audit": "SUS (research heuristic retained; exact official provenance checked separately)"}


def validate_failure(record):
    if (record.get("format") != "scanner-eval-structure-disposition" or record.get("version") != 1
            or record.get("candidate_id") != CANDIDATE or record.get("status") not in ("unavailable", "failed")
            or record.get("fallback") is not False or record.get("quality_measured") is not False
            or record.get("resources_measured") is not False or record.get("adoption") != "defer"
            or not record.get("stage") or not record.get("reason") or not record.get("inputs")
            or not record.get("evidence") or record.get("source_commit") != COMMIT
            or record.get("packages") != PACKAGES or record.get("models") != json.loads(json.dumps(MODELS))
            and record.get("models") != MODELS or record.get("configuration") != CONFIG or record.get("limits") != LIMITS):
        raise ValueError("invalid unavailable disposition")


def validate_isolation(proof):
    expected = {"uid": 10001, "gid": 10001, "no_new_privs": 1,
                "capabilities": [0, 0, 0, 0, 0], "network_denied": True,
                "input_read_only": True, "root_read_only": True, "cpu_seconds": 900,
                "rss_limit": 2 << 30, "file_size": 1 << 30, "processes": 64,
                "open_files": 256, "explicit_environment": True, "threads": 2}
    if proof != expected:
        raise ValueError("isolation self-check failed")


def load_runtime(root, lock, expected_sha256):
    if lock.stat().st_size > 4 << 20 or digest(lock) != expected_sha256:
        raise ValueError("changed runtime configuration")
    value = json.loads(lock.read_text())
    if (value.get("candidate_id") != CANDIDATE or value.get("configuration") != CONFIG
            or value.get("source_commit") != COMMIT or value.get("packages") != PACKAGES
            or value.get("limits") != LIMITS or value.get("complete") is not True
            or not value.get("files") or not value.get("wheels") or not value.get("licenses")):
        raise ValueError("incomplete runtime identity/license closure")
    for ref in value["files"]:
        verify_ref(root, ref)
    return value


def fetch(url, target, expected=None, maximum=200 << 20):
    """Registry/source acquisition only; downloaded code is never imported here."""
    if not url.startswith(("https://pypi.org/", "https://files.pythonhosted.org/",
                           "https://raw.githubusercontent.com/docling-project/",
                           "https://api.github.com/repos/docling-project/",
                           "https://huggingface.co/docling-project/",
                           "https://download.pytorch.org/whl/cpu/",
                           "https://raw.githubusercontent.com/roniemartinez/latex2mathml/",
                           "https://api.github.com/repos/roniemartinez/latex2mathml/")):
        raise ValueError("nonofficial acquisition URL")
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    hasher, size = hashlib.sha256(), 0
    with urllib.request.urlopen(url, timeout=30) as response, target.open("xb") as stream:
        while chunk := response.read(1 << 20):
            size += len(chunk)
            if size > maximum:
                raise ValueError("acquisition byte limit")
            hasher.update(chunk)
            stream.write(chunk)
    target.chmod(0o600)
    if expected and hasher.hexdigest() != expected:
        raise ValueError("official input digest mismatch")
    return {"path": str(target), "sha256": hasher.hexdigest(), "bytes": size, "url": url}


def inspect_wheel(path, metadata, supplemental=None):
    entries = [x for x in metadata["urls"] if x["filename"] == path.name]
    if len(entries) != 1 or digest(path) != entries[0]["digests"]["sha256"]:
        raise ValueError("wheel is not an exact official PyPI byte identity")
    with zipfile.ZipFile(path) as wheel:
        if sum(i.file_size for i in wheel.infolist()) > 1 << 30:
            raise ValueError("oversized wheel")
        names = wheel.namelist()
        meta = [n for n in names if n.count("/") == 1 and n.endswith(".dist-info/METADATA")]
        if len(meta) != 1:
            raise ValueError("wheel METADATA missing")
        body = wheel.read(meta[0])
        parsed = email.parser.BytesParser().parsebytes(body)
        canonical = lambda name: re.sub(r"[-_.]+", "-", name).lower()
        if (canonical(parsed["Name"]) != canonical(metadata["info"]["name"])
                or parsed["Version"] != metadata["info"]["version"]):
            raise ValueError("wheel METADATA identity mismatch")
        licenses = [n for n in names if not n.endswith("/") and ".dist-info/" in n
                    and re.search(r"(license|copying|notice)", n, re.I)]
        if not licenses and not supplemental:
            raise ValueError("wheel license bytes missing")
        refs = []
        for name in [meta[0]] + licenses:
            if Path(name).is_absolute() or ".." in Path(name).parts:
                raise ValueError("unsafe wheel member")
            data = wheel.read(name)
            destination = path.parent.parent / "notices" / path.name / name
            destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            destination.write_bytes(data)
            destination.chmod(0o600)
            refs.append({"path": str(destination), "sha256": hashlib.sha256(data).hexdigest()})
        return {"name": parsed["Name"], "version": parsed["Version"], "sha256": digest(path),
                "license": parsed.get("License-Expression") or parsed.get("License"),
                "notices": refs + (supplemental or [])}


def resolve_and_inspect(root, wheelhouse):
    """Resolve official wheels without importing them; record every chosen byte."""
    if wheelhouse is None:
        wheelhouse = root / "wheels"
        command = [sys.executable, "-m", "pip", "download", "--disable-pip-version-check", "--no-cache-dir",
                   "--only-binary=:all:", "--index-url", "https://pypi.org/simple", "--extra-index-url",
                   "https://download.pytorch.org/whl/cpu", "--dest", str(wheelhouse),
                   "docling-slim[convert-core,format-pdf,models-local]==2.149.0",
                   "docling-core==2.106.0", "docling-ibm-models==4.0.4", "torch==2.9.1+cpu", "torchvision==0.24.1+cpu"]
        with (root / "dependency-resolution.log").open("x") as log:
            subprocess.run(command, check=True, stdout=log, stderr=subprocess.STDOUT, timeout=600,
                           env={"PATH": "/usr/bin:/bin", "HOME": str(root), "PIP_CONFIG_FILE": "/dev/null"})
        write_json(root / "dependency-command.json", command)
    elif wheelhouse.resolve() != (root / "wheels").resolve():
        for path in wheelhouse.glob("*.whl"):
            destination = root / "wheels" / path.name
            if destination.exists():
                if digest(destination) != digest(path):
                    raise ValueError("changed cached wheel")
            else:
                shutil.copyfile(path, destination)
        wheelhouse = root / "wheels"
    inspections = []
    for wheel in sorted(wheelhouse.glob("*.whl")):
        with zipfile.ZipFile(wheel) as archive:
            meta = next(n for n in archive.namelist() if n.count("/") == 1 and n.endswith(".dist-info/METADATA"))
            parsed = email.parser.BytesParser().parsebytes(archive.read(meta))
        name, version = parsed["Name"], parsed["Version"]
        if "+cpu" in version:
            index = root / "metadata" / (name + "-cpu.html")
            fetch("https://download.pytorch.org/whl/cpu/" + name + "/", index)
            matches = [html.unescape(u) for u in re.findall(r'href="([^"]+)"', index.read_text())
                       if urllib.parse.unquote(urllib.parse.urlsplit(html.unescape(u)).path).endswith("/" + wheel.name)]
            if len(matches) != 1 or not matches[0].startswith(("https://download.pytorch.org/whl/cpu/",
                                                            "https://download-r2.pytorch.org/whl/cpu/")):
                raise ValueError("CPU wheel official index identity missing")
            wanted = urllib.parse.parse_qs(urllib.parse.urlsplit(matches[0]).fragment).get("sha256", [])
            if len(wanted) != 1:
                raise ValueError("CPU wheel digest missing")
            metadata = {"info": {"name": name, "version": version},
                        "urls": [{"filename": wheel.name, "digests": {"sha256": wanted[0]}}]}
        else:
            path = root / "metadata" / (name + "-" + version + ".json")
            fetch(f"https://pypi.org/pypi/{name}/{version}/json", path)
            metadata = json.loads(path.read_text())
        supplemental = []
        if name == "latex2mathml":
            # Exact tagged upstream terms supplement the wheel/sdist packaging omission.
            tag = root / "metadata" / "latex2mathml-tag.json"
            fetch(f"https://api.github.com/repos/roniemartinez/latex2mathml/git/ref/tags/{version}", tag)
            obj = json.loads(tag.read_text())["object"]
            if obj["type"] == "tag":
                annotated = root / "metadata" / "latex2mathml-annotated-tag.json"
                fetch(obj["url"], annotated)
                obj = json.loads(annotated.read_text())["object"]
            if obj["type"] != "commit":
                raise ValueError("license source does not resolve to a commit")
            commit = obj["sha"]
            supplemental.append(fetch(f"https://raw.githubusercontent.com/roniemartinez/latex2mathml/{commit}/LICENSE",
                                      root / "licenses" / "latex2mathml-LICENSE"))
        if name == "tokenizers":
            import tarfile
            source = next(s for s in metadata["urls"] if s["packagetype"] == "sdist")
            path = root / "sources" / source["filename"]
            fetch(source["url"], path, source["digests"]["sha256"])
            with tarfile.open(path) as archive:
                member = archive.getmember(f"tokenizers-{version}/tokenizers/LICENSE")
                data = archive.extractfile(member).read(1 << 20)
            destination = root / "licenses" / "tokenizers-LICENSE"
            destination.parent.mkdir(mode=0o700, exist_ok=True)
            destination.write_bytes(data)
            supplemental.append({"path": str(destination), "sha256": digest(destination), "source_sha256": digest(path)})
        inspections.append(inspect_wheel(wheel, metadata, supplemental))
    if not inspections or len(inspections) > 128:
        raise ValueError("empty or oversized dependency closure")
    write_json(root / "wheel-inspections.json", inspections)
    requirements = "\n".join(f'{i["name"]}=={i["version"]} --hash=sha256:{i["sha256"]}' for i in inspections) + "\n"
    (root / "requirements.lock").write_text(requirements)
    return wheelhouse, inspections


SANDBOX = r'''
mount --make-rprivate /
jail=$1; evidence=$2; writable=$3; pythonbase=$4; retained=$5; ocrroot=$6; inputdir=$7; shift 7
mount -t tmpfs -o size=16m,mode=0700 tmpfs "$jail"
ln -s usr/bin "$jail/bin"
bind_ro() { mkdir -p "$jail$1"; mount --bind "$1" "$jail$1"; mount -o remount,bind,ro "$jail$1"; }
bind_ro /usr
bind_ro /lib
if [ -d /lib64 ]; then bind_ro /lib64; fi
bind_ro "$pythonbase"
bind_ro "$evidence"
bind_ro "$retained"
bind_ro "$ocrroot"
if [ -n "$inputdir" ]; then bind_ro "$inputdir"; fi
mkdir -p "$jail$writable" "$jail/proc" "$jail/tmp" "$jail/dev"
mount --bind "$writable" "$jail$writable"
mount -t tmpfs -o size=1g,mode=0700 tmpfs "$jail/tmp"
mount -t proc proc "$jail/proc"
touch "$jail/dev/null" "$jail/dev/urandom"
mount --bind /dev/null "$jail/dev/null"
mount --bind /dev/urandom "$jail/dev/urandom"
mount -o remount,ro "$jail"
exec /usr/bin/prlimit --cpu=900:900 --rss=2147483648:2147483648 --fsize=1073741824:1073741824 --nproc=64:64 --nofile=256:256 /usr/sbin/chroot "$jail" /usr/bin/setpriv --no-new-privs --bounding-set=-all --inh-caps=-all --ambient-caps=-all /usr/bin/env -i PATH=/usr/bin:/bin LC_ALL=C HOME=/tmp TMPDIR=/tmp OMP_NUM_THREADS=2 OPENBLAS_NUM_THREADS=2 MKL_NUM_THREADS=2 HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 PYTHONDONTWRITEBYTECODE=1 "$@"
'''


def sandbox_command(jail, root, writable, retained, argv, source=None):
    inputs = json.loads((root / "inputs.json").read_text())
    ocrroot = Path(inputs["source"]).parent.parent
    return ["/usr/bin/unshare", "--user", "--map-user=10001", "--map-group=10001", "--keep-caps",
            "--net", "--mount", "--pid", "--fork", "/bin/sh", "-ec", SANDBOX, "structure-sandbox",
            str(jail), str(root), str(writable), sys.base_prefix, str(retained), str(ocrroot),
            str(source.parent) if source else "", *map(str, argv)]


SELF_CHECK = r'''
import errno,json,os,resource,socket
from pathlib import Path
status=dict(line.split(':',1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
def ro(path):
    try:
        with open(path,'a') as stream: pass
    except OSError as error: return error.errno == errno.EROFS
    return False
denied=False
try:
    with socket.create_connection(('192.0.2.1',443),timeout=1): pass
except OSError as error: denied=error.errno in (errno.ENETUNREACH,errno.EACCES,errno.EPERM)
proof={'uid':os.getuid(),'gid':os.getgid(),'no_new_privs':int(status['NoNewPrivs']),
       'capabilities':[int(status[k],16) for k in ('CapInh','CapPrm','CapEff','CapBnd','CapAmb')],
       'network_denied':denied,'input_read_only':ro(os.environ['CHECK_INPUT']),
       'root_read_only':ro('/should-not-write'),
       'cpu_seconds':resource.getrlimit(resource.RLIMIT_CPU)[0],
       'rss_limit':resource.getrlimit(resource.RLIMIT_RSS)[0],
       'file_size':resource.getrlimit(resource.RLIMIT_FSIZE)[0],
       'processes':resource.getrlimit(resource.RLIMIT_NPROC)[0],
       'open_files':resource.getrlimit(resource.RLIMIT_NOFILE)[0],
       'explicit_environment':set(os.environ)=={'PATH','LC_ALL','HOME','TMPDIR','OMP_NUM_THREADS','OPENBLAS_NUM_THREADS','MKL_NUM_THREADS','HF_HUB_OFFLINE','TRANSFORMERS_OFFLINE','PYTHONDONTWRITEBYTECODE','CHECK_INPUT'},
       'threads':int(os.environ['OMP_NUM_THREADS'])}
print(json.dumps(proof))
'''


def frozen_inputs(phase46, catalog, oracle):
    lock = phase46 / "tesseract-5.5.4-runtime.json"
    value = json.loads(lock.read_text())
    for ref in value["files"]:
        verify_ref(phase46, ref)
    tesseract = Path(value["tesseract"])
    wanted = next(r["sha256"] for r in value["files"] if r["path"] == "tools/scanner-native-tesseract")
    if value["version"] != "5.5.4" or digest(tesseract) != wanted:
        raise ValueError("missing or changed retained Tesseract")
    prior = json.loads((phase46 / "final-manifest.json").read_text())
    if digest(oracle) != prior["oracle_sha256"]:
        raise ValueError("changed frozen oracle")
    fixture = next(f for f in json.loads(catalog.read_text())["fixtures"] if f["id"] == "fixture_ocr_table_001")
    source = phase46 / "corpus" / Path(fixture["recipe"]["source"]).name
    if digest(source) != fixture["sha256"] or source.stat().st_size != fixture["bytes"]:
        raise ValueError("changed frozen table")
    return {"catalog_sha256": digest(catalog), "oracle_sha256": digest(oracle),
            "fixture_id": fixture["id"], "fixture_sha256": fixture["sha256"],
            "source_pages": fixture["page_ids"], "source": str(source),
            "tesseract": str(tesseract), "tesseract_sha256": wanted,
            "retained_runtime_lock_sha256": digest(lock)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--phase46-root", type=Path, required=True)
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--oracle", type=Path, required=True)
    parser.add_argument("--wheelhouse", type=Path, help="Reuse downloads; every wheel is reverified against its exact official index")
    args = parser.parse_args()
    os.umask(0o077)
    root = args.root.resolve()
    root.mkdir(mode=0o700)
    inputs = frozen_inputs(args.phase46_root.resolve(), args.catalog.resolve(), args.oracle.resolve())
    write_json(root / "inputs.json", inputs)
    stage = "source"
    retained = []
    try:
        tag = root / "source-tag.json"
        retained.append(fetch("https://api.github.com/repos/docling-project/docling/git/ref/tags/v2.149.0", tag))
        if json.loads(tag.read_text())["object"]["sha"] != COMMIT:
            raise ValueError("official tag commit mismatch")
        for name in ("LICENSE", "pyproject.toml", "docling/datamodel/pipeline_options.py",
                     "docling/datamodel/layout_model_specs.py", "docling/datamodel/accelerator_options.py",
                     "docling/models/stages/table_structure/table_structure_model.py"):
            retained.append(fetch(f"https://raw.githubusercontent.com/docling-project/docling/{COMMIT}/{name}", root / "source" / name))
        stage = "wheel-provenance"
        for name, version in PACKAGES.items():
            path = root / "metadata" / (name + ".json")
            retained.append(fetch(f"https://pypi.org/pypi/{name}/{version}/json", path))
            metadata = json.loads(path.read_text())
            if not any("github.com/docling-project/" in u for u in metadata["info"]["project_urls"].values()):
                raise ValueError("official project link mismatch")
            wheels = [w for w in metadata["urls"] if w["filename"].endswith("-py3-none-any.whl")]
            if len(wheels) != 1:
                raise ValueError("exact pinned wheel missing")
            wheel = wheels[0]
            local = root / "wheels" / wheel["filename"]
            retained.append(fetch(wheel["url"], local, wheel["digests"]["sha256"]))
            write_json(root / (name + "-inspection.json"), inspect_wheel(local, metadata))
        stage = "model-config-provenance"
        for repo, revision, path, expected in MODELS:
            if path.endswith(".safetensors"):
                continue
            retained.append(fetch(f"https://huggingface.co/docling-project/{repo}/resolve/{revision}/{path}",
                                  root / "models" / repo / path, expected))
        for repo, revision in (("docling-models", TABLE_REV), ("docling-layout-heron", LAYOUT_REV)):
            retained.append(fetch(f"https://huggingface.co/docling-project/{repo}/resolve/{revision}/README.md",
                                  root / "models" / repo / "README.md"))
        stage = "dependency-closure"
        wheelhouse, wheels = resolve_and_inspect(root, args.wheelhouse)
        retained.append({"path": str(root / "wheel-inspections.json"), "sha256": digest(root / "wheel-inspections.json")})
        stage = "isolation"
        assembled = root / "assembled"
        assembled.mkdir(mode=0o700)
        retained_root = Path(inputs["tesseract"]).parent.parent
        with tempfile.TemporaryDirectory(prefix="scanner47-jail-") as jail:
            command = sandbox_command(jail, root, assembled, retained_root,
                                      ["/usr/bin/env", "CHECK_INPUT=" + str(root / "inputs.json"), sys.executable, "-I", "-c", SELF_CHECK])
            probe = subprocess.run(command, env={"PATH": "/usr/bin:/bin"}, capture_output=True, text=True, timeout=20, check=True)
            proof = json.loads(probe.stdout)
            write_json(root / "isolation.json", {"command": command, "exit_code": probe.returncode, "proof": proof})
            validate_isolation(proof)
            stage = "model-weights"
            for repo, revision, path, expected in MODELS:
                if path.endswith(".safetensors"):
                    retained.append(fetch(f"https://huggingface.co/docling-project/{repo}/resolve/{revision}/{path}",
                                          root / "models" / repo / path, expected, maximum=256 << 20))
            # Keep the exact layouts expected by the selected upstream pipeline.
            (root / "models" / "docling-models").rename(root / "models" / "docling-project--docling-models")
            (root / "models" / "docling-layout-heron").rename(root / "models" / "docling-project--docling-layout-heron")
            stage = "offline-install"
            create = sandbox_command(jail, root, assembled, retained_root,
                                     [sys.executable, "-I", "-m", "venv", "--copies", assembled / "venv"])
            with (root / "offline-install.log").open("x") as log:
                subprocess.run(create, check=True, stdout=log, stderr=subprocess.STDOUT, env={"PATH": "/usr/bin:/bin"}, timeout=60)
                install = sandbox_command(jail, root, assembled, retained_root,
                                          [assembled / "venv/bin/python", "-I", "-m", "pip", "install", "--no-index",
                                           "--find-links", wheelhouse, "--require-hashes", "-r", root / "requirements.lock"])
                subprocess.run(install, check=True, stdout=log, stderr=subprocess.STDOUT, env={"PATH": "/usr/bin:/bin"}, timeout=300)
        stage = "runtime-lock"
        tools = root / "tools"
        tools.mkdir(mode=0o700)
        for path in (Path(__file__), Path(__file__).with_name("candidate.py")):
            shutil.copyfile(path, tools / path.name)
        shutil.copyfile(sys.executable, tools / "python")
        (tools / "python").chmod(0o700)
        files = [{"path": str(p.relative_to(root)), "sha256": digest(p)} for p in root.rglob("*") if p.is_file()]
        runtime = {"candidate_id": CANDIDATE, "complete": True, "configuration": CONFIG, "limits": LIMITS,
                   "source_commit": COMMIT, "packages": PACKAGES, "wheels": wheels,
                   "licenses": [n for w in wheels for n in w["notices"]], "files": files,
                   "phase46_root": str(args.phase46_root.resolve()), "tesseract": inputs["tesseract"],
                   "retained_root": str(retained_root), "python_base": sys.base_prefix,
                   "python": str(assembled / "venv/bin/python"), "inputs": inputs}
        write_json(root / "runtime.json", runtime)
        config = {"format": "scanner-eval-candidate", "version": 1, "candidate_id": CANDIDATE,
                  "candidate_version": "2.149.0", "executable": str(tools / "python"),
                  "executable_sha256": digest(tools / "python"), "argv": [str(tools / "candidate.py")],
                  "environment": ["STRUCTURE_ROOT=" + str(root), "STRUCTURE_LOCK_SHA256=" + digest(root / "runtime.json")],
                  "sources": [{"name": "Docling", "version": COMMIT, "license": "MIT", "sha256": digest(root / "source/pyproject.toml")}],
                  "dependencies": [], "parameters": {"configuration_sha256": hashlib.sha256(json.dumps(CONFIG,sort_keys=True).encode()).hexdigest()},
                  "operator_steps": ["Verify exact official acquisition and offline isolation proof", "Run one frozen synthetic table with the existing bounded scanner-eval harness"]}
        write_json(root / "candidate.json", config)
        print(json.dumps({"status": "prepared", "candidate": str(root / "candidate.json")}))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        evidence = {"error_type": type(error).__name__, "detail": str(error)[:1024], "retained": retained}
        if isinstance(error, subprocess.CalledProcessError):
            evidence.update(exit_code=error.returncode, stderr=(error.stderr or "")[:4096], command=error.cmd)
        record = failure_record(stage, str(error)[:512], inputs, evidence)
        validate_failure(record)
        write_json(root / "disposition.json", record)
        print(json.dumps({"status": "unavailable", "stage": stage, "disposition": str(root / "disposition.json")}))
        return 2


if __name__ == "__main__":
    sys.exit(main())
