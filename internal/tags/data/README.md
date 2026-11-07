# 标签翻译数据

- 来源：https://github.com/EhTagTranslation/Database
- 生成方式：仓库根目录执行 `python3 scripts/update_tags.py`（取最新 release 的 `db.text.json`，只保留标签中文名）。
- 许可：数据库文本内容以 CC BY-NC-SA 3.0 协议提供，详见上游 `LICENSE.md`。本目录仅保存由其派生的标签名映射。
- 当前版本见 `tags.json` 的 `version` 字段；更新后二进制内嵌数据随之变化，无需联网。
