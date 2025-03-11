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