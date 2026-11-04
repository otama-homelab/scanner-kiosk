"""Check the shared palette used by text and visible input/control borders."""
from pathlib import Path
import re
import unittest

CSS = Path(__file__).resolve().parents[1] / 'web/public/style.css'


def contrast(a, b):
    def luminance(value):
        value = value.removeprefix('#')
        if len(value) == 3:
            value = ''.join(c * 2 for c in value)
        rgb = [int(value[i:i + 2], 16) / 255 for i in (0, 2, 4)]
        rgb = [v / 12.92 if v <= .04045 else ((v + .055) / 1.055) ** 2.4 for v in rgb]
        return sum(a * b for a, b in zip(rgb, (.2126, .7152, .0722)))
    low, high = sorted((luminance(a), luminance(b)))
    return (high + .05) / (low + .05)


class PaletteContrastTests(unittest.TestCase):
    def palettes(self):
        css = CSS.read_text()
        light = re.search(r':root\{([^}]+)\}', css).group(1)
        dark = re.search(r':root\[data-theme=dark\]\{([^}]+)\}', css).group(1)
        for theme, block in [('light', light), ('dark', light + ';' + dark)]:
            yield theme, dict(re.findall(r'--([\w-]+):\s*(#[0-9a-fA-F]{3,6})(?=;|$)', block))

    def test_text_palette(self):
        self.assertAlmostEqual(contrast("#000", "#fff"), 21)
        for theme, palette in self.palettes():
            for foreground in ['text', 'muted']:
                for background in ['bg', 'surface', 'surface-raised', 'accent-soft']:
                    with self.subTest(theme=theme, foreground=foreground, background=background):
                        self.assertGreaterEqual(contrast(palette[foreground], palette[background]), 4.5)
            for background in ['accent', 'accent-hover']:
                with self.subTest(theme=theme, background=background):
                    self.assertGreaterEqual(contrast(palette['on-accent'], palette[background]), 4.5)

    def test_input_border_palette(self):
        # Read the actual shared rule so a token change cannot leave the test stale.
        rule = re.search(r"button,select,input\{([^}]+)\}", CSS.read_text()).group(1)
        border = re.search(r"border:1px solid var\(--([\w-]+)\)", rule).group(1)
        for theme, palette in self.palettes():
            for background in ['bg', 'surface', 'surface-raised', 'accent-soft']:
                with self.subTest(theme=theme, background=background):
                    self.assertGreaterEqual(contrast(palette[border], palette[background]), 3)


if __name__ == '__main__':
    unittest.main()
