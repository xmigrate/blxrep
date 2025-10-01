## Lab Environment Setup

Follow these steps to create and use two Vagrant VMs: `agent` and `dispatcher`.

### 1) Install prerequisites
- **Vagrant (AMD64)**: Download and install the Windows AMD64 package from the official site: [Vagrant Install](https://developer.hashicorp.com/vagrant/install)
- **VirtualBox**: Download and install VirtualBox for Windows: [VirtualBox Downloads](https://www.virtualbox.org/wiki/Downloads)

After installing both, **restart your machine**.

### 2) Bring up the VMs
Run these commands from the repository root:

```bash
cd lab
vagrant up
```

The first run may take several minutes while the base box downloads.

### 3) Connect to the VMs
- **Dispatcher**:

```bash
vagrant ssh dispatcher
```

- **Agent**:

```bash
vagrant ssh agent
```

### Notes
- The default provider is VirtualBox; ensure virtualization is enabled in BIOS/UEFI.
- On Windows, run commands in PowerShell or Command Prompt.

