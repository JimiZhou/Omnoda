# Omnoda

Every Device. One Mind.

Omnoda 把你自己的电脑收成一个私人执行网络。节点只主动连出，控制面负责任务和 DAG，节点本地策略决定能不能执行。

英文说明、已验证平台和限制以 [README.md](README.md) 为准。本文不重复宣称尚未在 macOS、Windows 或 ARM 真机上跑过的结果。

## 启动

```bash
go build -o bin/omnoda-server ./cmd/omnoda-server
go build -o bin/omnoda ./cmd/omnoda
cd apps/web && npm install && npm run build && cd ../..
./bin/omnoda-server -addr 127.0.0.1:8787 -public-url http://127.0.0.1:8787
```

打开 http://127.0.0.1:8787 ，首次访问创建管理员。没有内置密码。

## 添加节点

控制台生成一次性注册码（10 分钟，用后失效），然后：

```bash
curl -fsSL http://127.0.0.1:8787/install.sh | sh -s -- \
  --server http://127.0.0.1:8787 \
  --code 注册码
```

安装脚本会先校验发布签名，再运行二进制。开发机上也可以直接：

```bash
omnoda join --server http://127.0.0.1:8787 --code 注册码 --workspace "$PWD" --label role=monitor
omnoda start
```

## 工作流

聊天页默认使用 **Template planner**。包含 ARM、修复、测试等关键词的目标会生成诊断 → 修复 → 测试 DAG。健康则跳过修复。修复步骤需要针对该动作的审批。未配置模型时不会假装成 LLM 规划。

演示故障服务在 `fixtures/demo-service`，不要指向生产服务。
