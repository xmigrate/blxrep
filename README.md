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


## Installation

(Add installation instructions here, such as):

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
- `--device`: Device to use (required for agent mode, e.g., /dev/xvda)
- `--data-dir`: Data directory (required for dispatcher mode)
- `--sync-freq`: Sync frequency in minutes (required for dispatcher mode)

### Dispatcher Commands

blxrep provides additional commands for operating on the dispatcher such as restoring data from checkpoint.

#### Show Checkpoints
This command lists the checkpoints that can be used for restoration.

```bash
blxrep dispatcher show --agent=<agent_name> --data-dir=<data_directory> [--start=<YYYYMMDDHHMM>] [--end=<YYYYMMDDHHMM>]
```

#### Restore Checkpoint
This command is used for restoring and creating a disk image to the selected checkpoint.
```bash
blxrep dispatcher restore --checkpoint=<timestamp> --agent=<agent_name> --data-dir=<data_directory>
```

## Modes of Operation

### Dispatcher Mode

In dispatcher mode, blxrep manages the overall replication process. It acts as a central collector for replicating disk data from multiple servers. It requires a data directory and sync frequency to be specified. All types of disk backups are collected and stored in the specified data directory. Sync frequency is required to mention how frequently data should be synchronized at the source, i.e., the servers that are connected to the dispatcher. It forces the data to be written to the physical disk when it happens. It can add performance overhead if not configured wisely.

### Agent Mode

In agent mode, blxrep runs on individual servers to send snapshot backups and live changes to the dispatcher. It requires an agent ID, dispatcher address, and device to be specified. We need the agent ID to be unique if we are connecting multiple servers to the same dispatcher. Device is the disk that needs to be backed up and monitored for live changes.


