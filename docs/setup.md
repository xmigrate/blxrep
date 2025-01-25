# Setup guide
## Agent Setup

### Agent Prerequisites
- Requires a Linux kernel 5.10 or higher with eBPF support
- Only supports in Linux

### Agent Installation
=== "Debian/Ubuntu (.deb)"
    1. Download the package:
    ```bash {.copy}
    wget https://github.com/xmigrate/blxrep/releases/download/v0.0.7/blxrep-0.0.7-amd64.deb
    ```

    2. Install the package:
    ```bash {.copy}
    sudo dpkg -i blxrep-0.0.7-amd64.deb
    ```
    > Note: If you get an error about missing dependencies, you can install them with:
    ```bash {.copy}
    sudo apt-get install -f
    ```

=== "RedHat/CentOS (.rpm)"
    1. Download the package:
    ```bash {.copy}
    wget https://github.com/xmigrate/blxrep/releases/download/v0.0.7/blxrep-0.0.7-x86_64.rpm
    ```
    2. Install the package:
    ```bash {.copy}
    sudo rpm -i blxrep-0.0.7-x86_64.rpm
    ```

### Verify Installation
```bash {.copy}
sudo systemctl status blxrep
```
### Agent Configuration

Agent configuration file is located at `/etc/blxrep/config.yaml` by default.

Below is an example configuration file:

```yaml {.copy}
mode: "agent"
id: "hostname"
dispatcher-addr: "ip:port"
```

#### Configuration Parameters

| Parameter | Value | Description |
|-----------|--------|-------------|
| `mode` | `"agent"` | Specifies the operation mode |
| `id` | `"hostname"` | A unique identifier for the agent, usually the hostname |
| `dispatcher-addr` | `"ip:port"` | IP address and port of the dispatcher (default port: 8080) |

### Agent Post Installation and configuration

```bash {.copy}
sudo systemctl restart blxrep
sudo systemctl enable blxrep
```

## Dispatcher Setup

### Dispatcher Prerequisites
- Linux OS
- Additional disk mounted to a dedicated directory to store the full backups and incremental backups

### Dispatcher Installation

=== "Debian/Ubuntu (.deb)"
    1. Download the package:
    ```bash {.copy}
    wget https://github.com/xmigrate/blxrep/releases/download/v0.0.7/blxrep-0.0.7-amd64.deb
    ```

    2. Install the package:
    ```bash {.copy}
    sudo dpkg -i blxrep-0.0.7-amd64.deb
    ```
    > Note: If you get an error about missing dependencies, you can install them with:
    ```bash {.copy}
    sudo apt-get install -f
    ```

=== "RedHat/CentOS (.rpm)"
    1. Download the package:
    ```bash {.copy}
    wget https://github.com/xmigrate/blxrep/releases/download/v0.0.7/blxrep-0.0.7-x86_64.rpm
    ```
    2. Install the package:
    ```bash {.copy}
    sudo rpm -i blxrep-0.0.7-x86_64.rpm
    ```

### Verify Installation
```bash {.copy}
sudo systemctl status blxrep
```

### Dispatcher Configuration

Dispatcher configuration file is located at `/etc/blxrep/config.yaml` by default.

Below is an example configuration file:

```yaml {.copy}
mode: "dispatcher"
data-dir: "/data"
policy-dir: "/etc/blxrep/policies"
```

#### Configuration Parameters

| Parameter | Value | Description |
|-----------|--------|-------------|
| `mode` | `"dispatcher"` | Specifies the operation mode |
| `data-dir` | `"/data"` | Directory to store the full backups and incremental backups |
| `policy-dir` | `"/etc/blxrep/policies"` | Directory to store the backup policies |

### Backup policy

Backup policy is a YAML file that defines the backup schedule, retention policy, and other backup settings. It is located at `/etc/blxrep/policies` by default. You can create a new policy file by creating a new YAML file in this directory as you add new servers for backup.

Below is an example backup policy file:

```yaml {.copy}
name: "default-backup-policy"
description: "Backup policy for all servers"
archive_interval: 48h
snapshot_frequency: "daily"
snapshot_time: "12:00:00"
bandwidth_limit: 100
snapshot_retention: 30
live_sync_frequency: 2m
transition_after_days: 30
delete_after_days: 90

targets:
  # Range pattern
  - pattern: "*"
    disks_excluded: 
      - "/dev/xvdb"
```

#### Policy Parameters

| Parameter | Description |
|-----------|-------------|
| `name` | Name of the policy |
| `description` | Description of the policy |
| `archive_interval` | Interval to archive backups, eg 48h, 1d, 1w, 1m |
| `snapshot_frequency` | Frequency of the snapshots (daily, weekly, monthly) |
| `snapshot_time` | Time of the day to take the snapshots |
| `bandwidth_limit` | Bandwidth limit for the backup in MB/s |
| `snapshot_retention` | Number of days to keep the snapshots |
| `live_sync_frequency` | Frequency of the live sync |
| `transition_after_days` | Number of days to keep the full and incremental backups |
| `delete_after_days` | Number of days to keep the full backups and incremental backups |
| `targets` | List of targets to backup |
| `targets[].pattern` | Pattern of the target, eg "*" or "hostname" |
| `targets[].disks_excluded` | List of disks to exclude from the backup |

### Dispatcher Post Installation and configuration

```bash {.copy}
sudo systemctl restart blxrep
sudo systemctl enable blxrep
```
