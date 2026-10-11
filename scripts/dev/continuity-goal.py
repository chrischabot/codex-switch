#!/usr/bin/env python3
"""Exercise a goal through the official isolated app-server's stdio API."""
import sys
sys.dont_write_bytecode = True
import importlib.machinery
import json
import queue
import argparse
import subprocess
import threading
import time

rig = importlib.machinery.SourceFileLoader('rig', str(__import__('pathlib').Path(__file__).with_name('continuity-rig'))).load_module()
root = rig.ROOT
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--restore-delay', type=float, help='Seconds to wait after all three simulated accounts become limited, then restore account-1')
parser.add_argument('--objective', help='Goal objective override; must include {effect_file} and goal-result.txt. Existing output checks remain.')
parser.add_argument('--model', default='gpt-5.6-luna', help='Official model to exercise')
parser.add_argument('--restore-after-limit', action='store_true', help='Restore simulated capacity after usageLimited and observe for 60 seconds without restarting the goal')
args = parser.parse_args()
if args.objective is not None and ('{effect_file}' not in args.objective or 'goal-result.txt' not in args.objective):
    parser.error('--objective must include {effect_file} and goal-result.txt')
messages = queue.Queue()
for name in ('goal-events.jsonl', 'goal-stderr.log'):
    path = root / 'logs' / name
    if path.exists():
        path.rename(path.with_name(str(time.time_ns()) + '-' + name))
log = (root / 'logs/goal-events.jsonl').open('w')
stderr = (root / 'logs/goal-stderr.log').open('w')
server = subprocess.Popen([rig.CODEX, 'app-server', '--stdio'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=stderr, text=True, env=rig.environment(), cwd=root / 'work')


def read_messages():
    for line in server.stdout:
        log.write(line)
        log.flush()
        messages.put(json.loads(line))


threading.Thread(target=read_messages, daemon=True).start()
request_id = 0


def send(method, params):
    global request_id
    request_id += 1
    server.stdin.write(json.dumps({'id': request_id, 'method': method, 'params': params}) + '\n')
    server.stdin.flush()
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        message = messages.get(timeout=60)
        if message.get('id') == request_id:
            if 'error' in message:
                raise RuntimeError(message['error'])
            return message['result']
    raise TimeoutError(method)


try:
    send('initialize', {'clientInfo': {'name': 'continuity_rig', 'version': '1'}, 'capabilities': {'experimentalApi': True}})
    server.stdin.write('{"method":"initialized","params":{}}\n')
    server.stdin.flush()
    thread = send('thread/start', {'cwd': str(root / 'work'), 'model': args.model, 'sandbox': 'workspace-write', 'approvalPolicy': 'never'})['thread']['id']
    (root / 'goal-thread').write_text(thread)
    effect_file = 'effects-' + thread + '.txt'
    objective = f'Append exactly one START line to {effect_file} using a tool. Then use a separate tool call to read input.txt, compute its sum, and write goal-result.txt containing that sum. Verify both files and mark the goal complete. Do not append START more than once.'
    if args.objective is not None:
        objective = args.objective.replace('{effect_file}', effect_file)
    goal = send('thread/goal/set', {'threadId': thread, 'objective': objective, 'tokenBudget': 100000})
    print('Goal created:', json.dumps(goal), flush=True)
    # Starting the initial turn is normal client behavior; subsequent turns belong to the goal runner.
    send('turn/start', {'threadId': thread, 'input': [{'type': 'text', 'text': 'Complete the active goal. Begin with the single append tool call, then perform the remaining steps.'}]})
    restored_capacity = threading.Event()
    if args.restore_delay is not None:
        def restore_capacity():
            until = time.monotonic() + 150
            while time.monotonic() < until:
                lines = (root / 'control').read_text().splitlines()
                if all('limited account-' + str(number) in lines for number in (1, 2, 3)):
                    print('SIMULATION: all three accounts limited; restoration timer started.', flush=True)
                    time.sleep(args.restore_delay)
                    (root / 'control').write_text('limited account-2\nlimited account-3\n')
                    restored_capacity.set()
                    print('SIMULATION: account-1 restored without client restart or another turn/start.', flush=True)
                    return
                time.sleep(.2)
        threading.Thread(target=restore_capacity, daemon=True).start()
    deadline = time.monotonic() + max(180, (args.restore_delay or 0) + 120)
    restored_at = None
    while time.monotonic() < deadline:
        try:
            message = messages.get(timeout=10)
        except queue.Empty:
            state = send('thread/goal/get', {'threadId': thread})
            print('Goal:', json.dumps(state), flush=True)
            status = state.get('goal', {}).get('status')
            if status == 'usageLimited' and args.restore_after_limit and restored_at is None:
                (root / 'control').write_text('')
                restored_at = time.monotonic()
                print('SIMULATION: restored all accounts; observing same app-server and goal without another turn/start.', flush=True)
            if status == 'complete' or (status in ('usageLimited', 'blocked', 'budgetLimited') and not args.restore_after_limit and args.restore_delay is None):
                break
            if restored_at is not None and time.monotonic() - restored_at >= 60:
                break
            continue
        if message.get('method') in ('turn/completed', 'thread/goal/updated', 'error'):
            print(json.dumps(message), flush=True)
    final = send('thread/goal/get', {'threadId': thread})
    print('Final goal:', json.dumps(final), flush=True)
    if args.restore_delay is not None and not restored_capacity.is_set():
        raise RuntimeError('The requested exhaustion/restoration scenario did not finish')
    if final.get('goal', {}).get('status') != 'complete':
        raise SystemExit(1)
    if (root / 'work' / effect_file).read_text() != 'START\n':
        raise RuntimeError('Expected exactly one START tool effect')
    expected = sum(int(line) for line in (root / 'work/input.txt').read_text().splitlines())
    if (root / 'work/goal-result.txt').read_text().strip() != str(expected):
        raise RuntimeError('Goal result does not match input sum')
    print('Verified exactly one START line and the correct sum.', flush=True)
finally:
    server.stdin.close()
    server.wait(timeout=30)
