#!/usr/bin/env python3
"""从 EhTagTranslation/Database 生成 internal/tags/data/tags.json。

只保留「命名空间 -> 标签 -> 中文名」的映射，去掉简介等大字段。
用法：
    python3 scripts/update_tags.py                 # 自动取最新 release
    python3 scripts/update_tags.py db.text.json    # 使用本地已下载的文件
"""
import json
import subprocess
import sys
import urllib.request

RELEASE_API = "https://api.github.com/repos/EhTagTranslation/Database/releases/latest"
OUT = "internal/tags/data/tags.json"


def download(url, dest):
    req = urllib.request.Request(url, headers={"User-Agent": "telehentai-tags-gen"})
    with urllib.request.urlopen(req, timeout=120) as resp, open(dest, "wb") as f:
        f.write(resp.read())


def main():
    if len(sys.argv) > 1:
        source = sys.argv[1]
        version = "local"
    else:
        meta = json.load(urllib.request.urlopen(RELEASE_API, timeout=60))
        version = meta["tag_name"]
        asset = next(a for a in meta["assets"] if a["name"] == "db.text.json")
        source = "/tmp/db.text.json"
        download(asset["browser_download_url"], source)
        print(f"downloaded {version} -> {source}")

    raw = json.load(open(source, encoding="utf-8"))
    out = {"version": version, "namespaces": {}, "tags": {}}
    for ns in raw["data"]:
        if ns["namespace"] == "rows":
            for key, info in ns["data"].items():
                out["namespaces"][key] = info.get("name", "")
            continue
        table = {}
        for tag, info in ns["data"].items():
            name = (info.get("name") or "").strip()
            if name:
                table[tag] = name
        out["tags"][ns["namespace"]] = table

    text = json.dumps(out, ensure_ascii=False, separators=(",", ":"))
    with open(OUT, "w", encoding="utf-8") as f:
        f.write(text)
    count = sum(len(t) for t in out["tags"].values())
    print(f"wrote {OUT}: {count} tags, {len(text.encode())} bytes, version={version}")


if __name__ == "__main__":
    main()
