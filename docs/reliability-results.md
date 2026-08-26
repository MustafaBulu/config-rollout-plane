# Reliability Results

This report records local evidence from the reliability scenario suite. Results depend on local CPU
load and are intended as reproducible development evidence, not production capacity claims.

Command:

```bash
go run ./cmd/reliability -scenario all -concurrency 32
```

Recorded local run:

```text
scenario control-plane-restart: PASSED duration=5ms
  metric control_plane_starts=1
  metric registered_agents=1
  metric control_plane_restarts=1
  metric post_restart_heartbeats=1

scenario data-plane-outage: PASSED duration=7ms
  metric control_plane_starts=1
  metric data_plane_starts=1
  metric registered_agents=1
  metric cache_warm_configs=1
  metric data_plane_outages=1
  metric cache_reads_during_outage=1

scenario concurrent-rollout-acknowledgements: PASSED duration=112ms
  metric registered_agents=200
  metric rollout_targets=200
  metric concurrent_acknowledgements=200
  metric final_coverage_percent=100
  timing agent_registration_duration=28ms
  timing concurrent_ack_duration=83ms

scenario rollback-propagation-timing: PASSED duration=115ms
  metric registered_agents=120
  metric candidate_targets=120
  metric candidate_acknowledgements=120
  metric rollback_verification_acknowledgements=120
  timing rollback_propagation_duration=49ms
```

Evidence covered:

- concurrent rollout acknowledgement reaches 100 percent coverage and completes the rollout
- unhealthy guardrail starts rollback
- rollback verification propagates stable snapshots to all candidate targets
- data-plane outage leaves the warmed local cache readable
- control-plane restart preserves agent identity state when backing services remain available
