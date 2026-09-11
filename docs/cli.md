# The command line

Everything the web UI does, the command line does, against the same running
instance and the same state. `faultline serve` and `faultline run` start
Faultline; the commands here talk to one that is already running, over its HTTP
API.

```
faultline rule      list | add | rm | enable | disable
faultline scenario  list | on | off
faultline events    tail | export
faultline upstreams
```

Two commands are exceptions. `faultline init` writes a file and talks to
nobody, and `faultline run` starts an instance of its own around one command,
which [Sessions and reports](#sessions-and-reports) below covers.

## The first file

`faultline init` writes a commented `faultline.yaml` into the working
directory, holding one example rule and one example scenario, both turned off,
so starting Faultline right after it changes nothing about your traffic. The
hosts in it are an illustration: edit them to the ones your application calls.
[docs/config.md](config.md) is the reference for everything the file can say.

```
$ faultline init
wrote faultline.yaml
Nothing in it is on yet. Edit the hosts to the ones your application calls,
then run it through Faultline:

  faultline run -- <the command you already use to start your app>
```

A file that is already there is never overwritten. It is the one thing in the
directory Faultline cannot give back:

```
$ faultline init
faultline: faultline.yaml is already there, and init will not overwrite it; write another file with `faultline init <file>`, or replace this one with --force
```

`faultline init <file>` writes the file you name instead, which is how a
configuration that lives beside the repository rather than in it gets started,
and `--force` replaces one in place.

## Two flags everything takes

`--admin` is the address of the instance, `http://localhost:9000` unless you
say otherwise. A bare host and port is read as http, so `--admin
localhost:9100` works too.

`--json` prints the API's own JSON instead of a table. Use it for anything that
reads the output with a program; the tables are for people and their columns
are not a promise.

A command that cannot reach an instance says so in one line and exits non-zero:

```
$ faultline rule list
faultline: no Faultline is listening at http://localhost:9000; start one with `faultline serve`
```

A refusal from the API is printed as the sentence the API wrote, with the field
it named in brackets:

```
$ faultline rule add --host httpbin.org --fault delay --set ms=soon
faultline: ms must be a whole number (fault.ms)
```

## Rules

```
$ faultline rule list
ID               ENABLED  MATCH             FAULT            BEHAVIOR     NAME
httpbin-is-slow  yes      httpbin.org       delay ms=2000    -            Httpbin is slow
echo-is-down     yes      postman-echo.com  status code=503  first_n n=2  Echo is down
```

`rule add` writes one rule. The match has a flag per field, and both
catalogues - faults and behaviors - are reached the same way, by naming a type
and setting its parameters, so a fault added to Faultline later needs no new
flag here:

```
faultline rule add --host api.stripe.com --fault delay --set ms=2000

faultline rule add \
  --name "Charges fail twice" \
  --host api.stripe.com --method POST --path '/v1/charges/*' \
  --fault status --set code=503 \
  --behavior first_n --behavior-set n=2
```

| Flag | What it does |
| --- | --- |
| `--id` | the rule's id; left out, Faultline makes one from the name |
| `--name` | what the rule is for; left out, it is named after the fault and the host |
| `--host`, `--method`, `--path` | the match; anything left out matches everything |
| `--header name=value` | a request header the rule matches, repeatable |
| `--fault <type>` | the fault to inject |
| `--set name=value` | a fault parameter, repeatable |
| `--behavior <type>` | the behavior that gates the fault |
| `--behavior-set name=value` | a behavior parameter, repeatable |
| `--disabled` | add the rule turned off |
| `--from <file>` | read the whole rule as JSON instead, `-` for standard input |

A `--set` value is read as JSON when it is JSON, and as text when it is not.
`ms=2000` is the number, `set={"X-Test":"1"}` is a map, `remove=["Server"]` is
a list, and `body=not found` is the text. Quoting is how you write text that
looks like a number: `--set 'body="200"'`.

Faultline validates the parameters, not the command line, so the catalogue
never has to be described in two places. What comes back names the field:
`fault.ms`, `behavior.n`, `match.host`.

`--from` reads a whole rule as the API takes it, which is the way to write one
a script generated:

```
faultline rule add --from rule.json
jq '.rules[0]' scenarios.json | faultline rule add --from -
```

It replaces the flags above rather than mixing with them, and an absent
`enabled` means the same as it does over the API: the rule arrives on.

`rule enable` and `rule disable` take an id. Either one starts the rule's
behavior state over, so a `first_n: 2` rule fails its first two requests again.
`rule rm` takes one or more ids.

## Scenarios

Scenarios are declared in `faultline.yaml` and committed with the repository;
the command line turns them on and off.

```
$ faultline scenario list
NAME            ACTIVE  RULES
echo-throttled  no      echo-rate-limited

$ faultline scenario on echo-throttled
NAME            ACTIVE  RULES
echo-throttled  yes     echo-rate-limited
```

Only one scenario is on at a time, so turning one on turns off whichever was.
Turning one on also starts its rules' behavior state over, which is how you
rerun a rehearsal from the beginning: turn on the one that is already on.

## Sessions and reports

One `faultline run` is one session, and it ends with a report of what the
application actually did:

```
$ faultline run --scenario orders-flaky -- go run ./examples/go-client --count 3
admin: http://127.0.0.1:9000
config: faultline.yaml, watched for changes
proxy: http://127.0.0.1:9001
tls: intercepting HTTPS with CA "/home/you/.config/faultline/ca.crt"
bypass: localhost, 127.0.0.1, ::1
scenario: orders-flaky active for this run
2026/09/09 22:53:24 call 1: 503, 38 bytes, 10ms
2026/09/09 22:53:25 call 2: 503, 38 bytes, 0s
2026/09/09 22:53:27 call 3: 200, 273 bytes, 553ms

report: this run
REQUESTS  FAULTED  RETRIES  MAX RETRY WAIT  ABANDONED
3         2        2        1002ms          0
```

`--scenario <name>` turns that scenario on before the child starts and off
again once it has exited, whether it passed, failed, or was interrupted.
Because activating a scenario also starts its rules' behavior state over, the
run begins the rehearsal from the beginning every time. A name the
configuration file does not declare is refused before the child starts:

```
$ faultline run --scenario ghost -- go test ./...
faultline: --scenario "ghost": faultline.yaml declares no scenario by that name; it has orders-flaky
```

The report is printed for every run, scenario or not: a session is a session,
and a run that faulted nothing still says how many calls went out. It goes to
stderr, because stdout belongs to the child, and the exit code is the child's
own, so a failing test suite still fails and still gets its report.

`--report <file>` also writes the report as the API's own JSON, which is what a
CI job wants:

```
$ faultline run --scenario orders-flaky --report report.json -- go test ./...
$ cat report.json
{
  "total": 3,
  "faulted": 2,
  "retries": 2,
  "max_retry_wait_ms": 1002,
  "abandoned": 0
}
```

The flag takes the file, not a format, because JSON is the only machine-readable
shape there is and a flag with one legal value is noise. It is the same document
`GET /api/sessions/current/report` returns, so a test that wants the numbers
while the run is still going can read them there instead.

Two things the numbers mean. **A retry is a second call to the same method and
path on the same upstream within five seconds of one that failed**, whoever made
it: a poll loop looks like a retrying client to this heuristic, and so does a
second run of the same request by hand. **`abandoned` counts a failed call only
once its five second window has closed**, so calls that failed in the last
seconds before the child exited are not counted there yet; the report is taken
the moment the child is gone rather than five seconds later.

## Events

`events tail` follows the live stream and prints a line per call until you
interrupt it. It is read-only - nothing you type reaches Faultline.

```
$ faultline events tail
TIME      METHOD  HOST                      PATH        STATUS       MS  TIER   RULE
22:34:55  GET     httpbin.org               /get           200     2582  plain  httpbin-is-slow
22:34:55  GET     postman-echo.com          /get           503        0  plain  echo-is-down
-- rules changed
```

`-- rules changed` is the notice that says a cached rule list is stale; read
`faultline rule list` again if you are holding one. With `--json` every message
is printed as the API sends it, one envelope per line, so a program can switch
on `type`.

`events export` writes what the ring buffer holds. The default is JSON Lines,
one event object per line:

```
$ faultline events export --host httpbin.org --faulted -o events.ndjson
wrote 2 events to events.ndjson
```

JSON Lines because the file is meant for another tool: it streams, it appends,
`wc -l` counts it, `grep` filters it, and `jq -c` reads it a line at a time
without holding the whole run in memory. Each line is the same object
`events tail --json` prints inside its envelope. `--json` writes the API's own
array instead, for a reader that wants one document.

`--host`, `--faulted` and `--limit` narrow what is written. `--faulted=false`
asks for the calls nothing touched.

## Upstreams

```
$ faultline upstreams
HOST              TIER   REQUESTS  FAULTED  ERRORS  LAST SEEN  NOTE
httpbin.org       plain  1         1        0       22:34:55   -
postman-echo.com  plain  3         3        1       22:35:41   -
```

The tier says how much Faultline could see: `plain`, `intercepted`, or
`encrypted`, which takes connection faults only. Faulted counts the requests a
rule acted on; errors counts the ones that went wrong - a 5xx, or a request
that never got a status at all - so a host that is failing on its own is
told apart from one Faultline is breaking on purpose. A 4xx is the upstream
answering and is not an error here. The note says when something is in the way
- a host on the bypass list, or a client that did not trust the Faultline CA,
which [docs/trust.md](trust.md) explains.

## In tests

The commands are a thin layer over the Go client in
[clients/go](../clients/go), which is the same client the CLI uses. A test that
wants to add a rule, exercise its own code and assert on what Faultline
observed can use it directly rather than shelling out.

## For coding agents

`faultline mcp` serves the same capabilities to an agent over the Model Context
Protocol, on stdio:

```
claude mcp add faultline -- faultline mcp
```

It talks to the instance at `--admin` like every other command here. The same
tools are also served at `/mcp` on the admin port. See
[agents.md](agents.md).
