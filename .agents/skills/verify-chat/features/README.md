# Chat feature map

| User symptom | Map | Runnable coverage |
| --- | --- | --- |
| Sending a message produces no reply | [Roundtrip](roundtrip.md) | `run chat.roundtrip` |
| A reply disappears after reopening | [History](history.md) | Successful-turn history in `chat.roundtrip`; mid-stream interruption in `chat.reliability` |
| Tool approval or rejection behaves incorrectly | [Tools](tools.md) | `run chat.tools` through the official Go SDK |
| Busy rejection, idle retry or replacement Worker behaves incorrectly | [Recovery](recovery.md) | `run chat.reliability` |
| Another API instance cannot stream the reply | [Recovery](recovery.md) | `run chat.instances` |
| Public Session startup fails | [Public startup](public-start.md) | `run chat.public` with real Runner and local Docker sandbox |
| Chat latency regresses | [Performance](performance.md) | `run chat.performance --baseline REPORT` |

The upstream model is scripted. External cloud allocation, automatic cloud fault detection, browser rendering, real model quality and concurrent capacity remain outside these scenarios. Each scenario's final report must pass; a successful individual stage is insufficient.
