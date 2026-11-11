from pathlib import Path
import cv2,numpy as np,json,hashlib,shutil
root=Path('/tmp/scanner-fairscan-parity.43pB9h');old=Path('/tmp/scanner-fairscan-fullcpu.AzeA6f/source');repo=Path('/home/yamato/otama-homelab/scanner-kiosk');out=repo/'docs/design/research/correction-fairscan-mask-parity-v060';h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();rows=[]
for kind in ['images','masks']:(root/'dataset'/kind).mkdir(parents=True,exist_ok=True)
for image in sorted((old/'evaluation/dataset/images').glob('*.jpg')):
 id=image.stem;shutil.copyfile(image,root/'dataset/images'/image.name);maskfile=old/'evaluation/dataset/masks'/(id+'.png');mask=cv2.imread(str(maskfile),cv2.IMREAD_UNCHANGED);assert mask.dtype==np.uint8 and mask.shape==(256,256)
 ret,binary=cv2.threshold(mask,127,255,cv2.THRESH_BINARY);dest=root/'dataset/masks'/maskfile.name;assert cv2.imwrite(str(dest),binary);assert set(np.unique(binary)).issubset({0,255});assert np.array_equal(cv2.imread(str(dest),cv2.IMREAD_UNCHANGED),binary)
 rows.append({'id':id,'image_sha256':h(image),'original_mask_sha256':h(maskfile),'original_dtype':str(mask.dtype),'original_min':int(mask.min()),'original_max':int(mask.max()),'original_unique_values':len(np.unique(mask)),'binary_mask_sha256':h(dest),'binary_min':int(binary.min()),'binary_max':int(binary.max()),'foreground_pixels':int(np.count_nonzero(binary)),'total_pixels':int(binary.size),'official_threshold':0.5,'native_threshold_png_space':ret,'native_op':'cv2.threshold(...,127,255,THRESH_BINARY)','exact_android_parity':False})
assert len(rows)==6
sources={}
for p in [old/'app/src/main/java/org/fairscan/app/domain/ImageSegmentation.kt',old/'evaluation/python/generate_masks.py',old/'evaluation/src/main/java/org/fairscan/evaluation/DatasetEvaluator.kt',old/'imageprocessing/src/main/java/org/fairscan/imageprocessing/DocumentDetection.kt']:sources[str(p.relative_to(old))]=h(p)
(out/'mask-bindings.json').write_text(json.dumps({'source_revision':'2c5230b12f69f3cee2cc5a970013da20848d1272','sources':sources,'rows':rows},indent=2)+'\n');print('Fixed0.5contract binary6masks prepared')
