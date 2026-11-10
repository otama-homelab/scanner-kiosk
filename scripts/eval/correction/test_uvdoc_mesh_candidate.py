"""Boundary and independent color/position oracles for the official mesh path."""
import unittest

import numpy as np

from uvdoc_mesh_candidate import validate_mesh


class MeshBoundaryTest(unittest.TestCase):
    def test_rejects_bad_shape_nonfinite_and_excessive_coordinates(self):
        for mesh in (np.zeros((2, 4, 4)), np.zeros((1, 3, 4, 4)),
                     np.zeros((1, 2, 4, 4), np.int64),
                     np.zeros((1, 2, 4, 4), np.float64),
                     np.zeros((1, 2, 1, 4)), np.zeros((1, 2, 513, 4)),
                     np.full((1, 2, 4, 4), np.nan),
                     np.full((1, 2, 4, 4), np.inf),
                     np.full((1, 2, 4, 4), 5.)):
            with self.assertRaises(ValueError):
                validate_mesh(mesh)
        validate_mesh(np.zeros((1, 2, 4, 4), np.float32))

    def test_official_sampling_preserves_rgb_and_known_source_positions(self):
        import torch
        from transformers.models.uvdoc.image_processing_uvdoc import UVDocImageProcessor

        processor = UVDocImageProcessor()
        rgb = np.zeros((5, 7, 3), np.uint8)
        rgb[:, :, 0] = 255
        rgb[2, 3] = [0, 255, 0]
        original_bgr = torch.from_numpy(rgb[:, :, ::-1].copy()).permute(2, 0, 1).float() / 255
        identity = torch.tensor([[[[-1., 1.], [-1., 1.]],
                                   [[-1., -1.], [1., 1.]]]])
        validate_mesh(identity.numpy())
        actual = processor.post_process_document_rectification(identity, [original_bgr])[0]['images']
        np.testing.assert_allclose(actual.numpy(), rgb, atol=1)
        center = torch.zeros(1, 2, 2, 2)
        sampled = processor.post_process_document_rectification(center, [original_bgr])[0]['images']
        np.testing.assert_array_equal(sampled.numpy(), np.broadcast_to([0, 255, 0], rgb.shape))


if __name__ == '__main__':
    unittest.main()
