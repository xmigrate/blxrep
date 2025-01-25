# Troubleshooting

## Agent not connecting to the dispatcher

If the agent is not connecting to the dispatcher, you can check the logs of the agent and dispatcher to see if there are any errors.

To check the logs of the agent, you can use the following command:

```bash
journalctl -xeu blxrep -f
```

To check the logs of the dispatcher, you can use the following command:

```bash
tail -f <dispatcher-data-dir>/logs/blxrep.log
```

If you see any errors, you can try to fix them by checking the documentation or asking for help in the community.

