#!/bin/bash
systemctl daemon-reload
systemctl enable blxrep
systemctl start blxrep
