"""Terminal checkbox: arrow keys move, space toggles, enter confirms. Standard library only."""
import os
import sys

UP, DOWN, SPACE, ENTER, ABORT = 'up', 'down', 'space', 'enter', 'abort'


def apply_key(state, key):
    """state = (cursor, selected set, count). Returns (state, done)."""
    cursor, selected, count = state
    if key == UP:
        cursor = (cursor - 1) % count
    elif key == DOWN:
        cursor = (cursor + 1) % count
    elif key == SPACE:
        selected = selected ^ {cursor}
    elif key == ENTER and selected:
        return (cursor, selected, count), True
    elif key == ABORT:
        raise KeyboardInterrupt
    return (cursor, selected, count), False


def read_key():
    if os.name == 'nt':
        import msvcrt
        ch = msvcrt.getwch()
        if ch in ('\x00', '\xe0'):
            return {'H': UP, 'P': DOWN}.get(msvcrt.getwch())
    else:
        import termios
        import tty
        fd = sys.stdin.fileno()
        old = termios.tcgetattr(fd)
        try:
            tty.setraw(fd)
            ch = sys.stdin.read(1)
            if ch == '\x1b':
                seq = sys.stdin.read(2)
                return {'[A': UP, '[B': DOWN}.get(seq)
        finally:
            termios.tcsetattr(fd, termios.TCSADRAIN, old)
    return {' ': SPACE, '\r': ENTER, '\n': ENTER, '\x03': ABORT, 'q': ABORT,
            'k': UP, 'j': DOWN}.get(ch)


def render(title, items, state, out, first):
    cursor, selected, _ = state
    if not first:
        out.write(f'\x1b[{len(items) + 1}A')  # back to the top of the list
    out.write('\x1b[2K' + title + '\n')
    for i, (key, label) in enumerate(items):
        mark = '[x]' if i in selected else '[ ]'
        pointer = '❯' if i == cursor else ' '
        out.write(f'\x1b[2K {pointer} {mark} {key:<11} {label}\n')
    out.flush()


def checkbox(title, items, out=sys.stdout):
    """items: [(key, label)]. Returns the selected keys in list order."""
    if os.name == 'nt':
        os.system('')  # enable ANSI escape handling in the Windows console
    state = (0, frozenset(), len(items))
    hint = f'{title}  (↑↓ 이동 · space 선택 · enter 확인 · q 취소)'
    render(hint, items, state, out, True)
    while True:
        state, done = apply_key(state, read_key())
        render(hint, items, state, out, False)
        if done:
            return [items[i][0] for i in sorted(state[1])]
