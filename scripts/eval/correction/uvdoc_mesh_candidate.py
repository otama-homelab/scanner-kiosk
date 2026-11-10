"""Evaluation-only glue for the unmodified Transformers UVDoc mesh pipeline."""
import hashlib
import inspect
import json
import os
from pathlib import Path
import sys
import time

import numpy as np
from PIL import Image

MODEL_REVISION = 'bfa6aad03b549c7e2cf89cefd2fb02ba3bef2ae4'
MODEL_FILES = {
    'model.safetensors': '65f980e3083b4919d962d7b4c8ddb42a926dc8b36f4909944746cf2d7b7325dc',
    'config.json': '7cab8c18905afaca325792ddba0dfc73a13dcadd886a5a7c008dabcb7fae7dd1',
    'preprocessor_config.json': 'd117e9226a945206cfb76a9d75d3ae5483080d90b48a34253626444b61771436',
}
MODEL_CODE = 'a544aa46d28b8d7aa5a41c78e3a9f0df770939baba3f1cfc4c34aa01294effb1'
PROCESSOR_CODE = '61399ca76db9ce5b9f475e730f9484ccd46fa16c2195e5394b45d97d8c2c2187'


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def validate_mesh(mesh):
    # Boundary validation only: out-of-source coordinates retain official zero padding.
    if (mesh.dtype != np.float32 or mesh.ndim != 4 or mesh.shape[:2] != (1, 2)
            or min(mesh.shape[2:]) < 2 or max(mesh.shape[2:]) > 512
            or not np.isfinite(mesh).all() or np.abs(mesh).max() > 4):
        raise ValueError('mesh limits')


def main():
    started = time.perf_counter()
    source = Path(os.environ['SCANNER_EVAL_INPUT'])
    output = Path(os.environ['SCANNER_EVAL_OUTPUT'])
    model_dir = Path(os.environ['EVAL_UVDOC_MESH_MODEL'])
    for name, expected in MODEL_FILES.items():
        path = model_dir / name
        if path.is_symlink() or sha256(path) != expected:
            raise ValueError('model identity')
    with Image.open(source) as image:
        width, height = image.size
        if (image.format != 'PNG' or min(width, height) < 1 or max(width, height) > 12000
                or width * height > 40_000_000):
            raise ValueError('input limits')
    source_hash = sha256(source)

    import torch
    from transformers import AutoImageProcessor, AutoModel
    from transformers.models.uvdoc.modeling_uvdoc import UVDocModel
    from transformers.models.uvdoc.image_processing_uvdoc import UVDocImageProcessor

    for obj, expected in ((UVDocModel, MODEL_CODE), (UVDocImageProcessor, PROCESSOR_CODE)):
        if sha256(Path(inspect.getfile(obj))) != expected:
            raise ValueError('upstream identity')
    torch.set_num_threads(2)
    torch.set_num_interop_threads(1)
    model = AutoModel.from_pretrained(model_dir, local_files_only=True,
                                     trust_remote_code=False).to('cpu').eval()
    processor = AutoImageProcessor.from_pretrained(model_dir, local_files_only=True,
                                                   trust_remote_code=False)
    ready = time.perf_counter()
    times, first = [], None
    for _ in range(3):
        tick = time.perf_counter()
        with Image.open(source) as image:
            inputs = processor(images=image.convert('RGB'), return_tensors='pt')
        with torch.inference_mode():
            prediction = model(pixel_values=inputs['pixel_values']).last_hidden_state
            mesh = prediction.cpu().numpy()
            validate_mesh(mesh)
            corrected = processor.post_process_document_rectification(
                prediction, inputs['original_images'])[0]['images'].cpu().numpy()
        if corrected.shape != (height, width, 3) or corrected.dtype != np.uint8:
            raise ValueError('output limits')
        # Official implementation returns RGB after its input BGR conversion and final flip.
        Image.fromarray(corrected).save(output / 'corrected.png')
        times.append(round((time.perf_counter() - tick) * 1000, 3))
        pair = (sha256(output / 'corrected.png'),
                hashlib.sha256(mesh.astype('<f4', copy=False).tobytes()).hexdigest())
        if first is not None and first != pair:
            raise ValueError('non-deterministic output')
        first = pair
    if sha256(source) != source_hash:
        raise ValueError('source changed')
    binding = {
        'source_sha256': source_hash, 'source_size': [width, height],
        'output_sha256': first[0], 'output_size': [width, height],
        'mesh_sha256': first[1], 'mesh_shape': list(mesh.shape),
        'mesh_dtype': '<f4', 'mesh': mesh.tolist(),
        'mesh_hash_encoding': 'float32-little-endian-c-order',
        'mesh_min': float(mesh.min()), 'mesh_max': float(mesh.max()),
        'coordinates': 'normalized-backward-xy', 'align_corners': True,
        'interpolation': 'bilinear', 'padding': 'zeros',
        'model_revision': MODEL_REVISION, 'model_files': MODEL_FILES,
        'model_code_sha256': MODEL_CODE, 'processor_code_sha256': PROCESSOR_CODE,
        'apply_eligible': False,
    }
    (output / 'mesh-binding.json').write_text(json.dumps(binding, indent=2) + '\n')
    parameters = {
        'transform_kind': 'nonlinear-mesh', 'map_status': 'source-bound-evaluation-only',
        'source_sha256': source_hash, 'mesh_sha256': first[1],
        'mesh_binding_sha256': sha256(output / 'mesh-binding.json'),
        'output_width': str(width), 'output_height': str(height),
        'setup_ms': str(round((ready - started) * 1000, 3)),
        'first_full_image_ms': str(times[0]), 'warm_full_image_ms': json.dumps(times[1:]),
        'inference_passes': '3', 'fallback_reason': '', 'apply_eligible': 'false',
    }
    result = {
        'format': 'scanner-eval-candidate-result', 'version': 1,
        'candidate_id': os.environ['EVAL_CANDIDATE_ID'],
        'artifacts': [{
            'id': 'artifact_candidate_correction_001', 'role': 'correction',
            'path': 'corrected.png', 'media_type': 'image/png',
            'source_pages': os.environ['SCANNER_EVAL_SOURCE_PAGES'].split(','),
            'parents': [os.environ['SCANNER_EVAL_ORIGINAL_ID']],
            'algorithm': 'uvdoc-official-transformers',
            'algorithm_version': MODEL_REVISION, 'parameters': parameters,
        }, {
            'id': 'artifact_candidate_mesh_binding_001', 'role': 'generated',
            'path': 'mesh-binding.json', 'media_type': 'application/json',
            'source_pages': os.environ['SCANNER_EVAL_SOURCE_PAGES'].split(','),
            'parents': [os.environ['SCANNER_EVAL_ORIGINAL_ID']],
            'algorithm': 'uvdoc-mesh-provenance',
            'algorithm_version': MODEL_REVISION, 'parameters': parameters,
        }],
    }
    (output / 'result.json').write_text(json.dumps(result) + '\n')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        print('scanner-eval: UVDoc mesh candidate failed', file=sys.stderr)
        raise SystemExit(2)
