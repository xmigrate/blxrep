# blxrep

blxrep is a powerful tool designed for live data replication of disks over a network. It operates in two modes: dispatcher and agent, allowing for efficient and flexible disaster recovery setup.
blxrep tracks the changes that happen on disk at sector level using eBPF tracepoints.

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
  - [Starting blxrep](#starting-blxrep)
  - [Dispatcher Commands](#dispatcher-commands)
- [Modes of Operation](#modes-of-operation)
- [TUI mode](#tui-mode)

## Installation

You can install blxrep using the following command:


```bash
TBD
```

## Configuration

blxrep uses a configuration file located at `/etc/blxrep/config.yaml` by default. You can specify a different configuration file using the `--config` flag.

Example configuration:

```yaml
mode: "dispatcher"  # or "agent"
id: "agent1"  # required for agent mode
dispatcher-addr: "localhost:8080"  # required for agent mode
device: "/dev/xvda"  # required for agent mode
data-dir: "/data"  # required for dispatcher mode
sync-freq: 3  # sync frequency in minutes, required for dispatcher mode
```

## Usage

### Starting blxrep

To start blxrep, use the `start` command:

```bash
blxrep start [flags]
```

Flags:
- `--mode`: Start mode ('dispatcher' or 'agent')
- `--id`: Agent ID (required for agent mode)
- `--dispatcher-addr`: Dispatcher address (required for agent mode, format: host:port)
- `--data-dir`: Data directory (required for dispatcher mode)
- `--sync-freq`: Sync frequency in minutes (required for dispatcher mode)
- `--policy-dir`: Policy directory (required for dispatcher mode)
- `--config`: Configuration file (optional)


## Modes of Operation

### Dispatcher Mode

In dispatcher mode, blxrep manages the overall replication process. It acts as a central collector for replicating disk data from multiple servers. It requires a data directory and policy directory to be specified. All types of disk backups are collected and stored in the specified data directory. Policy directory is used to specify the policy for the disk backups for each agent.


### Agent Mode

In agent mode, blxrep runs on individual servers to send snapshot backups and live changes to the dispatcher. It requires an agent ID, dispatcher address, and device to be specified. We need the agent ID to be unique if we are connecting multiple servers to the same dispatcher. Device is the disk that needs to be backed up and monitored for live changes.

### TUI mode

blxrep uses tcell for the TUI. It is a terminal UI library for Go that is easy to use and highly customizable. It is used to interact with the dispatcher and agents. With TUI mode, you can navigate throught the agegnts that are connected to the dispatcher and see the status of the disk backups. You can also mount the disk backups to any available point in time and restore the files or partitions with the help of the TUI.

To start the TUI, use the `tui` command:

```bash
blxrep tui --data-dir=<data_directory>
```
