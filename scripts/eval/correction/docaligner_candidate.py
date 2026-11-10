"""Evaluation-only glue: official DocAligner decoding and capybara image warp."""
import hashlib
import inspect
import os
from pathlib import Path
import shutil
import sys

from PIL import Image

from common import write_result

MODEL_SHA256 = 'f4117b786e3a18470f3865c93f3c2bd69d9b998edd60f385574a5c665e79594e'
UPSTREAM_SHA256 = 'cb333575ccde501bc731a73de21f4fd05a96247550e45fea969e79d6cc1e4672'


def safe_quad(points, width, height):
    # Trust-boundary validation only; never repair or infer missing corners.
    import cv2
    import numpy as np
    quad = np.asarray(points, dtype=np.float32)
    return bool(quad.shape == (4, 2) and np.isfinite(quad).all()
                and (quad >= 0).all() and (quad[:, 0] <= width).all()
                and (quad[:, 1] <= height).all()
                and cv2.isContourConvex(quad)
                and cv2.contourArea(quad, oriented=True) >= width * height * .25)


def infer(source):
    import capybara as cb
    import cv2
    import numpy as np
    import onnxruntime as ort
    from docaligner.heatmap_reg.infer import preprocess, postprocess

    upstream = Path(inspect.getfile(preprocess))
    if hashlib.sha256(upstream.read_bytes()).hexdigest() != UPSTREAM_SHA256:
        raise ValueError('upstream identity')
    model = Path(os.environ['EVAL_DOCALIGNER_MODEL'])
    if model.stat().st_size != 4767987 or hashlib.sha256(model.read_bytes()).hexdigest() != MODEL_SHA256:
        raise ValueError('model identity')
    cv2.setNumThreads(2)
    image = cv2.imread(str(source), cv2.IMREAD_COLOR)
    if image is None:
        raise ValueError('image')
    height, width = image.shape[:2]
    options = ort.SessionOptions()
    options.intra_op_num_threads = 2
    options.inter_op_num_threads = 1
    session = ort.InferenceSession(str(model), sess_options=options, providers=['CPUExecutionProvider'])
    inputs = preprocess(image, img_size_infer=(256, 256), do_center_crop=False)
    heatmap = session.run(['heatmap'], inputs['input'])[0]
    if heatmap.shape != (1, 4, 128, 128) or not np.isfinite(heatmap).all():
        raise ValueError('heatmap')
    quad = np.asarray(postprocess(heatmap, inputs['img_size_ori']), dtype=np.float32)
    if not safe_quad(quad, width, height):
        return None, None
    # Upstream library owns sizing, interpolation and perspective transformation.
    size = cb.Polygon(quad).min_box_wh
    if min(size) < 1 or max(size) > 12000 or size[0] * size[1] > 40_000_000:
        return None, None
    corrected = cb.imwarp_quadrangle(image, quad, do_order_points=False)
    return corrected, quad.tolist()


def main():
    source = Path(os.environ['SCANNER_EVAL_INPUT'])
    output = Path(os.environ['SCANNER_EVAL_OUTPUT'])
    with Image.open(source) as image:
        width, height = image.size
        if image.format != 'PNG' or min(width, height) < 1 or max(width, height) > 12000 or width * height > 40_000_000:
            raise ValueError('input limits')
    mode = os.environ.get('EVAL_CORRECTION_MODE', 'model')
    if mode not in ('none', 'model'):
        raise ValueError('mode')
    reason = 'not-requested' if mode == 'none' else ''
    quad = None
    if mode == 'model':
        corrected, quad = infer(source)
        if corrected is None:
            reason = 'unsafe-or-missing-corners'
        else:
            import cv2
            if not cv2.imwrite(str(output / 'corrected.png'), corrected):
                raise ValueError('output')
            height, width = corrected.shape[:2]
    if reason:
        shutil.copyfile(source, output / 'corrected.png')
    write_result('docaligner-lcnet100' if not reason else 'none', '1.1.1', width, height,
                 fallback_reason=reason, crop_quad=quad)


if __name__ == '__main__':
    try:
        main()
    except Exception:
        # Candidate errors must not serialize private paths or image metadata.
        print('scanner-eval: correction candidate failed', file=sys.stderr)
        raise SystemExit(2)
