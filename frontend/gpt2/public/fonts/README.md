# 本地字体

全部字体采用 SIL Open Font License 1.1，与本目录许可证一起分发。字体使用本地 URL，无字体 CDN 请求。

| 字体 | 文件与版本 | 来源 | 许可证 |
|---|---|---|---|
| Inter | `InterVariable.woff2`，4.001，字体内版本含 git-9221beed3 | [官方文件](https://github.com/rsms/inter/blob/master/docs/font-files/InterVariable.woff2) | `OFL-Inter.txt` |
| 思源黑体简体中文版 | `SourceHanSansCN-VF.woff2`，2.005 | [官方文件](https://github.com/adobe-fonts/source-han-sans/blob/release/Variable/WOFF2/OTF/Subset/SourceHanSansCN-VF.otf.woff2) | `OFL-SourceHanSans.txt` |
| JetBrains Mono | `JetBrainsMono-Regular.woff2`、`JetBrainsMono-Bold.woff2`，2.305 | [官方文件目录](https://github.com/JetBrains/JetBrainsMono/tree/master/fonts/webfonts) | `OFL-JetBrainsMono.txt` |
| 更纱 Fixed SC | `SarasaFixedSC-Regular.woff2`、`SarasaFixedSC-Bold.woff2`，1.0.43 | [官方发布包](https://github.com/be5invis/Sarasa-Gothic/releases/tag/v1.0.43) | `OFL-Sarasa.txt` |

Inter 与思源黑体为可变字体，分别负责界面的拉丁文字、数字与中文。终端默认使用 JetBrains Mono，中文回退到更纱 Fixed SC；也可完整使用更纱 Fixed SC。

更纱字体从官方 `SarasaFixedSC-TTF-1.0.43.7z` 的 Regular、Bold TTF 转换为 WOFF2，使用 fontTools 4.62.1 与 Brotli 1.2.1；保留原字形、字符映射与字体名称。原压缩包保存在开发产物目录，不参与网页分发。

六个网页字体文件共 25,897,284 字节，约 24.70 MiB。界面字体约 7.96 MiB；终端字体约 16.74 MiB，在创建终端或显示字体预览时加载。CSS 使用 `font-display: swap`；终端首次创建等待字体加载后计算字符尺寸。
