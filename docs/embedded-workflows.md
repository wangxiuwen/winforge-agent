# 嵌入式开发工作流

WinForge 只提供通用 Windows 执行面，芯片支持由仓库内的脚本决定。

## 推荐布局

```text
C:\WinForge\
  workspaces\
    jieli-device-agent\
    beken-device-agent\
  toolchains\
    jieli\
    beken\
  artifacts\
```

Mac 负责编辑、Git 和发起任务；Windows 负责厂商工具链、USB 下载和串口。构建脚本应进入项目目录运行并把结果写入 workspace 内的 `artifacts/`，Mac 再下载产物。

不要把厂商工具链打进 WinForge Release。对于杰理，调用 SDK 官方 `download.bat`；对于 BK，调用对应 SDK 的 Windows 构建/下载入口。

