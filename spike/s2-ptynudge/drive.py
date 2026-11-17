import os, pty, sys, time, select, struct, fcntl, termios, pyte, json
# usage: drive.py vendor scenario [extra wrapper flags]
vendor, scen = sys.argv[1], sys.argv[2]
extra = sys.argv[3:]
SP = os.path.dirname(os.path.abspath(__file__))
cmds = {"claude":["claude"],"codex":["codex"],"copilot":["copilot"],"opencode":["opencode"],"cursor":["cursor-agent"]}
work = f"{SP}/work-{vendor}"; os.makedirs(work, exist_ok=True)
nf = f"{work}/nudge.txt"; lg = f"{work}/{scen}.log"
for f in (nf, lg):
    try: os.remove(f)
    except FileNotFoundError: pass
ROWS, COLS = 40, 120
screen = pyte.Screen(COLS, ROWS); stream = pyte.ByteStream(screen)
pid, fd = pty.fork()
if pid == 0:
    os.chdir(work)
    os.execv(f"{SP}/s2", [f"{SP}/s2","-nudge-file",nf,"-log",lg,"-idle","2s","-quiet","3s",*extra,"--",*cmds[vendor]])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
raw = bytearray()
def pump(sec):
    end = time.time()+sec
    while time.time() < end:
        r,_,_ = select.select([fd],[],[],0.1)
        if r:
            try: d = os.read(fd, 65536)
            except OSError: return False
            if not d: return False
            raw.extend(d)
            try: stream.feed(d)
            except Exception: pass
            # answer terminal queries (cursor position, DA) so TUIs don't stall
            if b"\x1b[6n" in d: os.write(fd, b"\x1b[1;1R")
            if b"\x1b[c" in d or b"\x1b[0c" in d: os.write(fd, b"\x1b[?62;c")
    return True
def show(tag):
    print(f"--- {vendor}/{scen}: {tag}")
    lines = [l.rstrip() for l in screen.display]
    while lines and not lines[-1]: lines.pop()
    print("\n".join(lines[-30:]))
def send(b): os.write(fd, b)
pump(10)
boot = {"claude":b"\x1b[B\r","codex":b"2\x15","cursor":b"a\x15","copilot":b"\r"}.get(vendor)
if boot and os.environ.get("NOBOOT") is None:
    send(boot); pump(8)
pump(6); show("settled")
if scen == "idle":
    open(nf,"w").write("S2PING reply with only the word ok\n")
    pump(12); show("after nudge")
elif scen in ("collide","collide-nohold"):
    for ch in "hello wor":
        send(ch.encode()); pump(0.05)
    open(nf,"w").write("S2PING reply with only the word ok\n")
    pump(8); show("human mid-typing, nudge pending (8s)")
    for ch in "ld":
        send(ch.encode()); pump(0.05)
    pump(1); show("human finished typing, no Enter")
    send(b"\r"); pump(12); show("after human Enter")
elif scen == "busy":
    for ch in "run the shell command sleep 15 then reply done":
        send(ch.encode()); pump(0.02)
    send(b"\r"); pump(4)
    open(nf,"w").write("S2PING reply with only the word ok\n")
    pump(40); show("busy: nudge pending during a 15s tool call")
elif scen == "typeduring":  # human types while idle-nudge is in flight (burst right at send)
    open(nf,"w").write("S2PING reply with only the word ok\n")
    # wait until the wrapper says it sent, then type immediately
    t0=time.time()
    while time.time()-t0 < 20:
        pump(0.1)
        if os.path.exists(lg) and "nudge sent" in open(lg).read(): break
    send(b"XYZ"); pump(10); show("typed XYZ right after nudge sent")
print("--- wrapper log"); print(open(lg).read() if os.path.exists(lg) else "(none)")
open(f"{work}/{scen}.raw","wb").write(raw)
try: send(b"\x03"); pump(1); send(b"\x03"); pump(1); send(b"\x04"); pump(1)
except OSError: pass
try: os.kill(pid, 9)
except ProcessLookupError: pass
