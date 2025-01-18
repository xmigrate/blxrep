# blxrep

blxrep is a powerful tool designed for live data replication of disks over a network. It operates in two modes: dispatcher and agent, allowing for efficient and flexible disaster recovery setup.
blxrep tracks the changes that happen on disk at sector level using eBPF tracepoints.

## Table of Contents

- [Overview](#overview)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
  - [Starting blxrep](#starting-blxrep)
  - [Dispatcher Commands](#dispatcher-commands)
- [Modes of Operation](#modes-of-operation)
- [TUI mode](#tui-mode)

## Overview

Traditionally, companies have relied on kernel modules for change block tracking and bitmap-based incremental backups. While functional, this approach has several limitations:

1. Complex kernel module development and maintenance requiring deep kernel expertise
2. Challenging debugging process due to kernel space operations
3. Limited testing capabilities in kernel space
4. Scalability constraints due to kernel-level implementation
5. Steep learning curve for kernel module development
6. System reboots required for kernel module loading and updates
7. Potential for system instability and security vulnerabilities due to unrestricted kernel access

blxrep modernizes this approach by leveraging eBPF tracepoints to track disk changes at the sector level. This brings several advantages:

1. Simplified development through eBPF's modern tooling, extensive documentation, and active community support
2. Enhanced debugging capabilities with user-space tools and eBPF maps
3. Comprehensive testing framework support
4. Better scalability through efficient event processing
5. More approachable learning curve with high-level eBPF programming interfaces
6. Dynamic loading without system reboots
7. Improved safety through eBPF's verifier and sandboxed execution environment

## Installation

### For Debian/Ubuntu based systems (.deb)

1. Download the package:
```bash
wget https://github.com/xmigrate/blxrep/releases/download/v0.0.2/blxrep-0.0.2-amd64.deb
```
2. Install the package:
```bash
sudo dpkg -i blxrep-0.0.2-amd64.deb
```
> Note: If you get an error about missing dependencies, you can install them with:
```bash
sudo apt-get install -f
```

### For Redhat/CentOS based systems (.rpm)

1. Download the package:
```bash
wget https://github.com/xmigrate/blxrep/releases/download/v0.0.2/blxrep-0.0.2-x86_64.rpm
```
2. Install the package:
```bash
sudo rpm -i blxrep-0.0.2-x86_64.rpm
```

##Verify the installation:
```bash
blxrep --help
```

Configuration file is located at `/etc/blxrep/config.yaml` by default.

## Uninstallation

To uninstall blxrep, use the following command:

For Debian/Ubuntu:
```bash
sudo dpkg -r blxrep
```

For Redhat/CentOS:
```bash
sudo rpm -e blxrep
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
