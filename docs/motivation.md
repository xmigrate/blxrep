---
title: Why did we build blxrep?
description: Why did we build blxrep? and that too with eBPF?
---
# Background and Motivation

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