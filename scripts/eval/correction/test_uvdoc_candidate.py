import unittest

import numpy as np

from uvdoc_candidate import input_tensor, output_image


class UVDocBoundaryTest(unittest.TestCase):
    def test_known_bgr_roundtrip_and_channel_order(self):
        bgr = np.array([[[0, 255, 127], [255, 0, 64]]], dtype=np.uint8)
        tensor = input_tensor(bgr)
        self.assertEqual(tensor.shape, (1, 3, 1, 2))
        np.testing.assert_array_equal(output_image(tensor, (1, 2)), bgr[:, :, ::-1])

    def test_invalid_inputs_and_nonfinite_wrong_shape_output(self):
        for value in (None, np.zeros((0, 2, 3), np.uint8), np.zeros((3, 3)),
                      np.zeros((2, 2, 3), np.float32)):
            with self.assertRaises(ValueError):
                input_tensor(value)
        for pred in (np.zeros((1, 3, 2, 3)), np.full((1, 3, 2, 2), np.nan),
                     np.full((1, 3, 2, 2), np.inf)):
            with self.assertRaises(ValueError):
                output_image(pred, (2, 2))


if __name__ == '__main__':
    unittest.main()
