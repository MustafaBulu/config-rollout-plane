# Scale Simulator

The scale simulator runs SafeConfig services in memory and exercises a progressive rollout with
virtual agents. It is intended to produce local scale evidence without creating 1000 Kubernetes pods.

The simulator seeds:

- one tenant
- one `payment.failure_rate` config definition
- stable version 1
- candidate version 2
- a 5/25/100 rollout
- deterministic virtual agent IDs

Run the default 1000-agent scenario:

```bash
go run ./cmd/simulator -agents 1000 -concurrency 64
```

Equivalent Make target:

```bash
make simulate
```

JSON output is available for storing benchmark evidence:

```bash
go run ./cmd/simulator -agents 1000 -concurrency 64 -format json
```

Sample run on a local Windows development machine:

```text
agents: 1000 registered
rollout: rollout_9cc78419e23a29b983794685 COMPLETED
duration: 167ms
throughput: agents=2428/s snapshots=7284/s acks=3146/s
snapshots: 3000 latency[min=0s avg=2.592ms p50=1.776ms p95=8.479ms max=17.595ms]
acks: 1296 latency[min=0s avg=198us p50=0s p95=705us max=3.439ms]
stage stage-5: targets=44 acked=44 coverage=100.00% next=DEPLOYING/stage-25 duration=17ms
stage stage-25: targets=252 acked=252 coverage=100.00% next=DEPLOYING/stage-100 duration=63ms
stage stage-100: targets=1000 acked=1000 coverage=100.00% next=COMPLETED/stage-100 duration=322ms
```

The exact duration and latency values depend on local CPU load. Stage target counts are deterministic
for a given agent count because the simulator uses stable agent IDs and fixed config version IDs.
