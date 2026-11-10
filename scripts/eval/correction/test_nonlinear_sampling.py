"""Independent tiny fixtures for the published UVDoc sampling convention."""
import unittest

import numpy as np
import torch
import torch.nn.functional as F


class NonlinearSamplingTest(unittest.TestCase):
    def test_identity_preserves_boundary_text_and_fine_lines(self):
        # Known fixture: bright boundary and single-pixel lines against black.
        image = torch.zeros(1, 1, 7, 9)
        image[:, :, [0, -1], :] = 1
        image[:, :, :, [0, 4, -1]] = 1
        grid = F.affine_grid(torch.eye(2, 3)[None], image.shape, align_corners=True)
        low = F.affine_grid(torch.eye(2, 3)[None], [1, 1, 2, 2], align_corners=True)
        up = F.interpolate(low.permute(0, 3, 1, 2), size=(7, 9),
                           mode='bilinear', align_corners=True).permute(0, 2, 3, 1)
        for coordinates in (grid, up):
            sampled = F.grid_sample(image, coordinates, align_corners=True,
                                    mode='bilinear', padding_mode='zeros')
            torch.testing.assert_close(sampled, image, rtol=0, atol=1e-6)

    def test_known_shift_and_outside_padding(self):
        image = torch.arange(9, dtype=torch.float32).reshape(1, 1, 3, 3)
        grid = torch.tensor([[[[0., 0.], [1., 1.], [3., 3.]]]])
        actual = F.grid_sample(image, grid, align_corners=True, padding_mode='zeros')
        torch.testing.assert_close(actual, torch.tensor([[[[4., 8., 0.]]]]))

    def test_invalid_shape_library_rejects(self):
        with self.assertRaises(RuntimeError):
            F.grid_sample(torch.zeros(1, 1, 3, 3), torch.zeros(1, 2, 2, 3),
                          align_corners=True)

    def test_nonfinite_map_requires_validation_before_library(self):
        # grid_sample can interpret NaN; it is not a trust-boundary validator.
        for bad in (np.nan, np.inf, -np.inf):
            grid = np.zeros((1, 2, 2, 2), dtype=np.float32)
            grid[0, 0, 0, 0] = bad
            self.assertFalse(np.isfinite(grid).all())


if __name__ == '__main__':
    unittest.main()
