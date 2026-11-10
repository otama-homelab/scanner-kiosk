"""Evaluation glue for the unmodified official Paddle UVDoc ONNX image export.

Tensor layout/normalization and output conversion follow PaddleX's
WarpRunnerPredictor and DocTrPostProcess. The ONNX graph owns all warping.
No production dependency or geometric mapping is supplied by this adapter.
"""
import hashlib
import json
import os
from pathlib import Path
import sys
import time

import cv2
import numpy as np

MODEL_SHA256 = '54cab30dc2cf347b4f3d6971c833b1c3d84f5ef17280f1f6d15796bff20e63d6'


def input_tensor(image):
    if (image is None or image.ndim != 3 or image.shape[2] != 3
            or image.dtype != np.uint8 or min(image.shape[:2]) < 1
            or max(image.shape[:2]) > 12000 or image.shape[0] * image.shape[1] > 40_000_000):
        raise ValueError('input limits')
    return (image.astype(np.float32) * np.float32(1. / 255)).transpose(2, 0, 1)[None]


def output_image(prediction, shape):
    if prediction.shape != (1, 3, *shape) or not np.isfinite(prediction).all():
        raise ValueError('output limits')
    # Same scaling, BGR-to-RGB and uint8 conversion as official postprocessor.
    return (prediction[0].transpose(1, 2, 0) * np.float32(255))[:, :, ::-1].astype(np.uint8)


def main():
    started = time.perf_counter()
    source = Path(os.environ['SCANNER_EVAL_INPUT'])
    output = Path(os.environ['SCANNER_EVAL_OUTPUT'])
    model = Path(os.environ['EVAL_UVDOC_MODEL'])
    if (model.is_symlink() or model.stat().st_size != 31684150
            or hashlib.sha256(model.read_bytes()).hexdigest() != MODEL_SHA256):
        raise ValueError('model identity')
    import onnxruntime as ort
    from PIL import Image
    cv2.setNumThreads(2)
    image = cv2.imread(str(source), cv2.IMREAD_COLOR)
    tensor = input_tensor(image)
    options = ort.SessionOptions()
    options.intra_op_num_threads = 2
    options.inter_op_num_threads = 1
    session = ort.InferenceSession(str(model), sess_options=options,
                                  providers=['CPUExecutionProvider'])
    ready = time.perf_counter()
    times = []
    first = None
    # Bounded same-session first/warm probe; outer Phase44 runner owns resources.
    for index in range(3):
        tick = time.perf_counter()
        current = cv2.imread(str(source), cv2.IMREAD_COLOR)
        pred = session.run(['fetch_name_0'], {'image': input_tensor(current)})[0]
        corrected = output_image(pred, current.shape[:2])
        Image.fromarray(corrected).save(output / 'corrected.png')
        times.append(round((time.perf_counter() - tick) * 1000, 3))
        if index == 0:
            first = hashlib.sha256((output / 'corrected.png').read_bytes()).hexdigest()
        elif hashlib.sha256((output / 'corrected.png').read_bytes()).hexdigest() != first:
            raise ValueError('non-deterministic repeated output')
    height, width = image.shape[:2]
    parameters = {
        'transform_kind': 'opaque-nonlinear', 'map_status': 'unavailable',
        'model_sha256': MODEL_SHA256, 'output_width': str(width), 'output_height': str(height),
        'setup_ms': str(round((ready - started) * 1000, 3)),
        'first_full_image_ms': str(times[0]), 'warm_full_image_ms': json.dumps(times[1:]),
        'inference_passes': '3', 'fallback_reason': '',
        'apply_eligible': 'false',
    }
    result = {
        'format': 'scanner-eval-candidate-result', 'version': 1,
        'candidate_id': os.environ['EVAL_CANDIDATE_ID'],
        'artifacts': [{
            'id': 'artifact_candidate_correction_001', 'role': 'correction',
            'path': 'corrected.png', 'media_type': 'image/png',
            'source_pages': os.environ['SCANNER_EVAL_SOURCE_PAGES'].split(','),
            'parents': [os.environ['SCANNER_EVAL_ORIGINAL_ID']],
            'algorithm': 'uvdoc-official-onnx',
            'algorithm_version': '3bcf535371727d11e783101f79a504c68848aae3',
            'parameters': parameters,
        }],
    }
    (output / 'result.json').write_text(json.dumps(result) + '\n')


if __name__ == '__main__':
    try:
        main()
    except Exception:
        print('scanner-eval: UVDoc candidate failed', file=sys.stderr)
        raise SystemExit(2)
