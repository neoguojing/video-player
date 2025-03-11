TARGET = videoplayer
export sdl?=1
export qsv?=1

current_dir := $(shell pwd)

ifeq ($(sdl),1)
	TAGS = sdl
	ifeq ($(qsv),0)
		TAGS += cuda
		export PATH := $(PATH):$(current_dir)/libs/windows/ffmpeg_cuda/bin
	else
		TAGS += qsv
		export PATH := $(PATH):$(current_dir)/libs/windows/ffmpeg_qsv/bin
	endif
else
	TAGS = cpu
endif

# 编译命令
win:
	echo $$PATH
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -tags "$(TAGS)" -o $(TARGET).exe
	rm -rf bundle
	ldd $(TARGET).exe | python bundle.py

linux:
ifeq ($(sdl),0)
	GOOS=linux GOARCH=amd64 go build -o $(TARGET)
else
	GOOS=linux GOARCH=amd64 go build -tags sdl -o $(TARGET)
endif

macos:
	GOOS=darwin GOARCH=amd64 go build -o $(TARGET)

# 清理编译生成的文件
clean:
	rm -f $(TARGET)
	rm -rf bundle