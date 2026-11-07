# 标签翻译数据

本目录的 `tags.json` 是 [EhTagTranslation/Database](https://github.com/EhTagTranslation/Database) 的衍生数据。

## 来源与生成

- 来源：EhTagTranslation/Database 的 release 资产 `db.text.json`
- 版本：见 `tags.json` 的 `version` 字段（当前 `v7.29653.1`）
- 生成：仓库根目录执行 `python3 scripts/update_tags.py`，只保留「命名空间 → 标签 → 中文名」与命名空间中文名，丢弃简介等大字段
- 内嵌：`go:embed` 编进二进制，运行时不联网

## 许可证与义务

本项目使用的数据以 **Creative Commons 署名-非商业性使用-相同方式共享 3.0（CC BY-NC-SA 3.0）** 提供，全文见本目录 `LICENSE.md`，上游声明见其仓库 `LICENSE.md` 与 `README.md`。

使用或再分发 `tags.json` 时必须：

1. **署名**：保留 EhTagTranslation 及其贡献者的出处、链接与许可证；
2. **非商业**：不得用于商业目的（含广告、付费功能、收费部署等）；
3. **相同方式共享**：修改或派生的标签映射必须继续以 CC BY-NC-SA 3.0 发布。

本仓库的源代码使用 MIT（根目录 `LICENSE`），**不覆盖**本目录的数据；详见根目录 `NOTICE`。

## 社区礼仪

上游 README 请求下游项目在使用其数据库前，于上游仓库提交一个带项目简介/地址的 Issue。这是社区约定而非法律要求。
