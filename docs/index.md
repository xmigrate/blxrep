<p align="center">
  <img src="assets/logo.svg" alt="blxrep logo" width="400"/>
</p>

# blxrep

blxrep is a powerful tool designed for live data replication of disks over a network. It operates in two modes: dispatcher and agent, allowing for efficient and flexible disaster recovery setup.
blxrep tracks the changes that happen on disk at sector level using eBPF tracepoints.
<script src="https://asciinema.org/a/SGxy4s73ZpbTvjxonyVBYGW1C.js" id="asciicast-SGxy4s73ZpbTvjxonyVBYGW1C" async="true"></script>
## Modes of Operation
blxrep can be run in three modes: dispatcher, agent, and TUI. Each mode has its own purpose and configuration. 

### Dispatcher Mode

In dispatcher mode, blxrep manages the overall replication process. It acts as a central collector for replicating disk data from multiple servers. It requires a data directory and policy directory to be specified. All types of disk backups are collected and stored in the specified data directory. Policy directory is used to specify the policy for the disk backups for each agent.

### Agent Mode

In agent mode, blxrep runs on individual servers to send snapshot backups and live changes to the dispatcher. It requires an agent ID, dispatcher address, and device to be specified. We need the agent ID to be unique if we are connecting multiple servers to the same dispatcher. Device is the disk that needs to be backed up and monitored for live changes.

### TUI mode
blxrep also provides a TUI mode to interact with dispatcher and agents.
we use tcell for the TUI. It is a terminal UI library for Go that is easy to use and highly customizable. With TUI mode, you can navigate throught the agents that are connected to the dispatcher and see the status of the disk backups. You can also mount the disk backups to any available point in time and restore the files or partitions with the help of the TUI.

To start the TUI, use the `tui` command:

```bash
blxrep tui --data-dir=<data_directory>
```
