<!--
See CONTRIBUTING.md / CONTRIBUTING.zh-CN.md. Delete what doesn't apply.
请先阅读 CONTRIBUTING.zh-CN.md，删除不适用的部分。
-->

Fixes #

## Cause and change / 原因和改动

<!-- What was wrong and why, then what this changes. 哪里有问题、为什么，然后说明改了什么。 -->

## How it was written / 编写方式

- [ ] By hand / 手写
- [ ] With AI / 用 AI 编写

<!-- With AI, both screenshots are required. 用 AI 编写时，下面两张截图都必须提供。 -->

**Model that wrote it / 编写模型** (newest flagship of its vendor, name visible / 该厂商最新旗舰模型，截图中能看到型号):

<!-- screenshot / 截图 -->

**Review by another vendor's model / 另一家厂商模型的审查** (on the final diff, model visible / 针对最终 diff，截图中能看到型号):

<!-- screenshot / 截图 -->

| Finding / 问题 | Fixed, or why not / 已修复，或为什么不适用 |
| --- | --- |
|  |  |

## Checks / 检查

- [ ] `go vet ./...` and `go test ./...`
- [ ] `cd frontend && bun run typecheck && bun run build`
- [ ] `CGO_ENABLED=0` builds for windows, darwin and linux
- [ ] A test that fails without this change / 有一个没有本改动就会失败的测试
- [ ] No TODOs, dead code or duplicated logic left / 没有遗留 TODO、死代码或重复逻辑

**Tried in the app / 实际试用** (what, on which OS, and what you couldn't check / 试了什么、在哪个系统上、哪些没能检查):
