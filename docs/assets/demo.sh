#!/bin/bash
# Records the terminal GIF in the README. Run it from anywhere with
# ./bin/faultline built and tmux, asciinema and agg installed:
#
#   docs/assets/demo.sh
#
# The top tmux pane is the Go example under `faultline run`; the bottom pane is
# a second shell adding rules to the same instance. The recording is made on a
# pseudo-terminal of a fixed size, so it comes out the same wherever it runs.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
cast=$(mktemp -t faultline-demo).cast
T="tmux -L faultline-demo"

cd "$repo"
export PATH="$repo/bin:$PATH"

$T kill-server 2>/dev/null || true
$T -f /dev/null new-session -d -s demo -x 130 -y 34 "env PS1='$ ' bash --norc --noprofile"
$T set -g window-size manual
$T set -g status off
$T set -g pane-border-style fg=colour240
$T set -g pane-active-border-style fg=colour240
$T split-window -v -l 13 "env PS1='$ ' bash --norc --noprofile"
$T select-pane -t 0

type_line() { # pane, text: type it a character at a time, then Enter
  local pane=$1 text=$2 i
  for ((i = 0; i < ${#text}; i++)); do
    $T send-keys -t "$pane" -l -- "${text:i:1}"
    sleep 0.035
  done
  sleep 0.4
  $T send-keys -t "$pane" Enter
}

(
  sleep 1.5
  type_line 0 "faultline run -- go run ./examples/go-client -every 1s -retries 2 -backoff 500ms"
  sleep 6
  $T select-pane -t 1
  sleep 0.6
  type_line 1 "faultline upstreams"
  sleep 3
  type_line 1 "faultline rule add --host httpbin.org --fault delay --set ms=2000"
  sleep 7
  type_line 1 "faultline rule rm delay-on-httpbin-org"
  sleep 1
  type_line 1 "faultline rule add --host httpbin.org --fault status --set code=503 --behavior first_n --behavior-set n=2"
  sleep 7
  type_line 1 "faultline session report"
  sleep 5
  $T detach-client
) &

# asciinema sizes the recording from the terminal it is started in, and a
# script has none, so it gets a 130 by 34 pseudo-terminal here.
python3 - "$cast" "$T" <<'PY'
import fcntl, os, pty, struct, sys, termios
cast, tmux = sys.argv[1], sys.argv[2].split()
pid, fd = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 34, 130, 0, 0))
    os.execvp("asciinema", ["asciinema", "rec", "--overwrite", "--command", " ".join(tmux + ["attach", "-t", "demo"]), cast])
while True:
    try:
        if not os.read(fd, 65536):
            break
    except OSError:
        break
os.waitpid(pid, 0)
PY
wait
$T kill-server 2>/dev/null || true
pkill -f "faultline run -- go run ./examples/go-client" 2>/dev/null || true

# Drop the tail from the moment tmux detached, and hold the last frame.
python3 - "$cast" <<'PY'
import json, sys
path = sys.argv[1]
lines = open(path).read().splitlines()
out = [lines[0]]
for line in lines[1:]:
    ev = json.loads(line)
    if ev[1] == "o" and ("\x1b[?1049l" in ev[2] or "[detached" in ev[2]):
        break
    out.append(line)
out.append(json.dumps([2.5, "o", ""]))
open(path, "w").write("\n".join(out) + "\n")
PY

agg --font-size 15 --theme monokai --line-height 1.3 --fps-cap 15 "$cast" "$here/demo.gif"
rm -f "$cast"
echo "wrote $here/demo.gif"
