# CLAUDE.md

本文件为 Claude Code（claude.ai/code）在此仓库中工作时提供指导。

## 仓库说明

个人 Web3 学习仓库。`docs/` 是 Obsidian 知识库，存放学习笔记；`src/` 预留给未来的合约代码与脚本。

## Obsidian 知识库（docs/）

`docs/` 目录是一个 Obsidian vault。所有 `.md` 文件使用标准的 Obsidian 双向链接（`[[页面名称]]`）。重命名或移动文件时注意不要破坏这些链接。

自定义主题存放在 `.obsidian/themes/` 下——请勿删除。

## 内容组织规范

- 笔记按主题分类：`eth/` 以太坊相关，`Uniswap/` Uniswap v2/v3/v4，`《精通以太坊》/` 读书笔记。
- `大纲.md` 文件作为各主题的索引/目录页。
- README 中定义了学习流程：先在 docs 中写清原理，再到 src 中做最小可运行验证，最后补充一条总结到文档。

## 权限

仓库允许 `WebFetch` 访问 `learnblockchain.cn` 和 `mp.weixin.qq.com`（配置在 `.claude/settings.local.json`），这两个是中文 Web3 内容的主要参考来源。
