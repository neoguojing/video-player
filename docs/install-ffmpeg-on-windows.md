# ffmpeg库编译安装及入门指南（Windows篇）
## ffmpeg简介
ffmpeg是一套跨平台的，用于音视频录制、转换、流化等操作的完善的解决方案，它是业界最负盛名的开源音视频框架之一。许多软件都是基于ffmpeg开发的，如格式工厂、各种xx影音等。

ffmpeg 是一套开源库以及命令行工具的集合，用户既可以使用命令行直接对音视频进行操作（CLI），也可以调用其开源库进行个性化的功能开发（SDK）。

如果要在自己的程序中使用 ffmpeg ，那么使用它的 SDK 是最好的选择。当前 ffmpeg 包含以下几个库：

libavcodec : 编/解码
libavfilter : 帧级操作（如添加滤镜）
libavformat : 文件 I/O 和 封装/解封装
libavdevice : 设备视频文件的封装/解封装
libavutil : 通用工具集合
libswresample : 音频重采样、格式处理、混音等
libpostproc : 预处理
libswscale : 色彩处理和缩放

## ffmpeg库在 Windows 下的安装
### 1.环境准备
- 安装并配置 MYSY2
- 安装 git
- ffmpeg 源码
- x264 源码
### 2. 安装 MSYS2 及编译工具链
MSYS2 的安装也非常省心，只需要到[ MSYS2 官网](https://www.msys2.org/) 下载.exe安装包，管理员身份运行安装即可。

注意安装盘必须是NTFS，路径要全使用 ACSII 字符，不能有空格。建议就安装在默认位置，如果不想装在 C 盘，就直接改下盘符，装在其他盘的根目录。

安装完毕后，开始菜单里就会有下面的程序：

![img.png](img.png)

点击它们就会启动一个 shell 窗口，Just like on Linux ! 这个 shell 程序默认是 Mintty，类似 Linux 系统中的 bash shell。MSYS2 支持多个编译器工具链，它们的环境是独立的（可以去安装文件夹查看），这里选择启动 MINGW64 （如果你也是64位系统的话）。

然后安装mingw64编译链和基本的依赖：

```azure
pacman -S mingw-w64-x86_64-toolchain  # mingw64编译工具链，win下的gcc
pacman -S base-devel    # 一些基本的编译工具
pacman -S yasm nasm     # 汇编器
```
安装完毕后，可以输入gcc -v查看 gcc 版本
添加安装路径到环境变量(用户变量和系统变量都要) D:\msys64\mingw64\bin

### 3. ffmpeg 源码下载
在 [ffmpeg 官网](https://ffmpeg.org/download.html) 下载源码，目前最新的版本是 6.1
### 4. x264 源码下载
官方建议使用 git 下载源码（下载压缩包再解压应该也是一样的）：

```azure
git clone https://code.videolan.org/videolan/x264.git
```
### 4. 编译和安装
将所有源码放到同一文件夹下便于管理，我把它们都统一放在一个叫 ffmpeg 的文件夹下。然后再建立各自的 install 文件夹存储编译好的库（当然你也可以选择其他任何地方的文件夹）
为了方便，将编译的命令写成脚本 build-x264.sh 和 build-ffmpeg.sh。当前文件夹的结构如下

![img_1.png](img_1.png)

### 4.1 编译 x264 库
`build-x264.sh`脚本内容如下：

```shell
#!/bin/sh
basepath=$(cd `dirname $0`;pwd)
echo ${basepath}

cd ${basepath}/x264-src   # 根据路径名称自行修改
pwd

set -x 

./configure --prefix=${basepath}/x264_install --enable-shared
make -j8
make install
```

注意第一行必须是` #!/bin/sh` ，才能被 MSYS2 的 shell 识别为可执行脚本。（亲测在 MSYS2 中`chmod`命令没有效果）

这几条命令中最重要的就是`./configure`命令，它的参数会指导编译器应该如何编译代码。这里 `--prefix` 参数指定了编译好的库文件的安装路径，可以自己任意指定。 `--enable-shared` 代表编译动态库。如果你需要静态库，那么需要加入 -enable-static 参数。

此外，make 命令的-j参数是指并行编译的线程数，可以根据你的 CPU 核数自行确定。

可以在源码文件夹下，通过 ./configure --help 命令查看所有可选参数。

在 MSYS2 的 shell 中，打开源码所在文件夹，并执行脚本：

```shell
cd /d/workspace/ffmpeg
./build-x264.sh
```
注意 MSYS2 中文件路径的写法，是以/d代表 D 盘，类似 Linux 的风格。
不出意外的话，等待片刻后就会在 x264_install 路径下看到编译好的库。其中 bin/libx264-164.dll 文件就是x264的动态库文件。

如果出现错误，可以先单独执行 .\configure 命令，然后再执行 make ，逐步查找错误原因。

### 4.2 编译 ffmpeg 库
`build-ffmpeg.sh`脚本内容如下：
```shell
#!/bin/sh
basepath=$(cd `dirname $0`;pwd)
echo ${basepath}

cd ${basepath}/ffmpeg-6.1-src
pwd
set -x
export PKG_CONFIG_PATH=${PKG_CONFIG_PATH}:/d/workspace/ffmpeg/x264_install/lib/pkgconfig
echo ${PKG_CONFIG_PATH}

./configure --prefix=${basepath}/ffmpeg_6.1_install \
--enable-gpl --enable-libx264 --disable-static --enable-shared \
--extra-cflags=-l${basepath}/x264_install/include --extra-ldflags=-L${basepath}/x264_install/lib

make -j8
make install
```
使用的命令与libx264类似，也是先 configure 再 make 。如果需要添加 x264 支持的话，需要注意以下几点：

加入 --enable-libx264 参数
指定 PKG_CONFIG_PATH 变量，告知编译器 x264 库的路径
指定 x264 库的头文件包含路径及动态库链接的路径
ffmpeg 可自定义的编译参数非常多，有需要可自行研究。

然后同样也是执行脚本即可：


最后将dll的目录加入环境变量

```shell
D:\workspace\ffmpeg\x264_install\bin
D:\workspace\ffmpeg\ffmpeg_6.1_install\bin
```

- 参考
1. [ffmpeg库编译安装及入门指南（Windows篇）- 2022年底钜献](https://www.cnblogs.com/midoq/p/16969756.html)

2. [音视频开发三：Windows环境下FFmpeg编译安装](https://blog.csdn.net/qq_38056514/article/details/129827722)
