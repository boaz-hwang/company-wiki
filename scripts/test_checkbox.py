"""Checkbox key handling without a terminal."""
import unittest

from checkbox import ABORT, DOWN, ENTER, SPACE, UP, apply_key


class CheckboxTests(unittest.TestCase):
    def run_keys(self, keys, count=4):
        state = (0, frozenset(), count)
        for key in keys:
            state, done = apply_key(state, key)
            if done:
                return sorted(state[1])
        return None

    def test_space_toggles_and_enter_confirms(self):
        self.assertEqual(self.run_keys([SPACE, DOWN, DOWN, SPACE, ENTER]), [0, 2])
        self.assertEqual(self.run_keys([SPACE, SPACE, DOWN, SPACE, ENTER]), [1])

    def test_cursor_wraps_and_enter_needs_a_selection(self):
        self.assertEqual(self.run_keys([UP, SPACE, ENTER]), [3])
        self.assertIsNone(self.run_keys([ENTER]))

    def test_abort_raises(self):
        with self.assertRaises(KeyboardInterrupt):
            self.run_keys([ABORT])
