from pathlib import Path
from PIL import Image
import PIL,io,json,hashlib,zipfile,numpy as np
repo=Path('/home/yamato/otama-homelab/scanner-kiosk');root=Path('/tmp/scanner-original-truth.vhhyGR');out=repo/'docs/design/research/correction-original-truth-followup-v060';h=lambda b:hashlib.sha256(b).hexdigest();manifest=json.loads((repo/'docs/design/research/correction-validation-v060/corpus-hash-check.json').read_text())['fixtures'];by={r['relative_path']:r for r in manifest};scans={};pairs=[];archives=[];checks=[]
for kind,expected in [('crop','30e385f4a8c7bb47ec93cb61c73e40d91ed390fbf86b22cf1c5e4e8898df4e81'),('scan','28b97e256c657a9a2d6320c54517cb378af13f5f3ca27bde0f915c6da02e2bd2')]:
 path=Path('/tmp/scanner-v040')/(kind+'.zip');hh=hashlib.sha256()
 with path.open('rb') as f:
  while block:=f.read(1024*1024):hh.update(block)
 assert hh.hexdigest()==expected
 with zipfile.ZipFile(path) as z:
  archives.append({'path':str(path),'bytes':path.stat().st_size,'sha256':expected,'png_member_count':sum(n.endswith('.png') for n in z.namelist()),'nonimage_members':[n for n in z.namelist() if not n.endswith(('.png','/'))],'newly_downloaded':False})
  ids=[(k,m) for k in [1,2,3] for m in [1,2]] if kind=='crop' else [(k,None) for k in [1,2,3]]
  for k,m in ids:
   member=f'crop/{k}_{m} copy.png' if kind=='crop' else f'scan/{k}.png';name=f'crop_{k}_{m}_copy.png' if kind=='crop' else f'scan_{k}.png';raw=z.read(member);local=Path('/tmp/scanner-v040/corpus')/name;body=local.read_bytes();assert h(body)==by[name]['sha256'];a=Image.open(io.BytesIO(raw));b=Image.open(io.BytesIO(body));assert a.size==b.size;argb=np.asarray(a.convert('RGB'));brgb=np.asarray(b.convert('RGB'));same=np.array_equal(argb,brgb);assert same
   alpha=np.asarray(a.getchannel('A')) if 'A' in a.getbands() else None
   metadata={'archive_mode':a.mode,'canonical_mode':b.mode,'size':list(a.size),'archive_exif_orientation':a.getexif().get(274),'canonical_exif_orientation':b.getexif().get(274),'archive_metadata_keys':sorted(a.info.keys()),'canonical_metadata_keys':sorted(b.info.keys()),'alpha_min':int(alpha.min()) if alpha is not None else None,'alpha_max':int(alpha.max()) if alpha is not None else None,'alpha_opaque_fraction':float(np.mean(alpha==255)) if alpha is not None else None}
   rec={'id':by[name]['id'],'source_archive':kind+'.zip','archive_member':member,'archive_member_bytes':len(raw),'archive_member_sha256':h(raw),'canonical_path':str(local),'canonical_bytes':len(body),'canonical_sha256':h(body),'archive_bytes_equal_canonical':raw==body,'decoded_RGB_equal':same,'decoded_RGB_sha256':h(argb.tobytes()),'metadata':metadata,'normalization_scope':'RGB projection and PNG reencoding; original archive identity differs; no position/geometry transform'};checks.append(rec)
   if kind=='scan':
    dest=root/('archive_scan_'+str(k)+'.png');dest.write_bytes(raw);scans[k]=dict(rec,raw_reference_path=str(dest))
   else:pairs.append({'photo_id':f'photo{k}_{m}','document_id':k,'capture_variant':m,'original':rec,'reference_document_id':k,'pairing_basis':'officialUVDocpredictionstrips copy suffix;officialevalScriptloops k,m againstscan/k.png','pixel_aligned_gt':False,'digits_labels_supplied':False,'thin_line_regions_supplied':False})
for p in pairs:p['reference']=scans[p['reference_document_id']]
assert len(checks)==9 and len(pairs)==6 and len(scans)==3
(out/'exact-original-pair-bindings.json').write_text(json.dumps({'archives':archives,'normalization_equality':'decoded RGB and dimensions only, not encoded byte identity; alpha/orientation separately recorded','pillow_version':PIL.__version__,'all9_original9_canonical_hashes_preserved':True,'canonical_to_archive_identity':checks,'photos':pairs,'scans':list(scans.values()),'mandatory_photo2_2':next(p for p in pairs if p['photo_id']=='photo2_2'),'paired_flat_reference_available':True,'independent_dense_geometry_or_content_labels_available':False},indent=2)+'\n')
(out/'initial-failed-byte-assumption.json').write_text(json.dumps({'actual_first_check':'assert archive_member_sha256 == canonical_fixture_sha256','result':'failed','first_member':'crop/1_1 copy.png','cause':'canonical corpus is RGB-projected PNG-reencoded derivative, not source archive bytes','investigation':'all9 decoded RGB and shape equal; encoded identities and alpha/orientation recorded separately','original_canonical_files_modified':False},indent=2)+'\n')
print('All9canonicalhashes preserved;all9decodedRGB/shapematch;paired6photo3scan bound')
for r in scans.values():print(r['archive_member'],r['metadata']['alpha_min'],r['metadata']['alpha_max'],r['metadata']['alpha_opaque_fraction'])
