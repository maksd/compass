# Metris

## Overview

Metris is a metering component that collects data and sends them to EDP.

## Configuration

| CLI argument | Environment variable | Description | Default value |
| -- | -- | -- | -- |
| `--edp-url` | **EDP_URL** | EDP base URL | `https://input.yevents.io` |
| `--edp-token` | **EDP_TOKEN** | EDP source token | None |
| `--edp-namespace` | **EDP_NAMESPACE** | EDP Namespace | None |
| `--edp-data-stream` | **EDP_DATASTREAM_NAME** | EDP data stream name | None |
| `--edp-data-stream-version` | **EDP_DATASTREAM_VERSION** | EDP data stream version | None |
| `--edp-data-stream-env` | **EDP_DATASTREAM_ENV** | EDP data stream environment | None |
| `--edp-timeout` | **EDP_TIMEOUT** | Time limit for requests made by the EDP client | `30s` |
| `--edp-buffer` | **EDP_BUFFER** | Number of events that the buffer can have | `100` |
| `--edp-workers` | **EDP_WORKERS** | Number of workers to send metrics | `5` |
| `--edp-event-retry` | **EDP_RETRY** | Number of retries for sending event | `5` |
| `--provider-poll-interval` | **PROVIDER_POLLINTERVAL** | Interval at which metrics are fetch | `1m` |
| `--provider-workers` | **PROVIDER_WORKERS** | Number of workers to fetch metrics | `10` |
| `--provider-buffer` | **PROVIDER_BUFFER** | Number of clusters that the buffer can have | `100` |
| `--provider-client-trace-level` | **PROVIDER_CLIENT_TRACE_LEVEL** | Provider client trace level (0=disabled, 1=headers, 2=body) | `0` |
| `--listen-addr` | **METRIS_LISTENADDR** | Address and port for the server to listen on | None |
| `--tls-cert-file` | **METRIS_TLSCERTFILE** | Path to TLS certificate file | None |
| `--tls-key-file` | **METRIS_TLSKEYFILE** | Path to TLS key file | None |
| `--profiler-port` | **METRIS_PROFILER_PORT** | Port to expose debugging information | None |
| `--config-file` | None | Location of the config file | None |
| `--kubeconfig` | **METRIS_KUBECONFIG** | Path to the Gardener kubeconfig file | `~/.kube/config` |
| `--log-level` | **METRIS_LOGLEVEL** | Logging level | `info` |
| `--version` | None | Print version information and quit | None |
