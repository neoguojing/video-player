import os, fileinput, shutil

bundleDirPath = os.path.abspath("bundle")
os.makedirs(bundleDirPath, exist_ok=True)

for dllInfo in fileinput.input():
    dllInfo = dllInfo.strip()
    dllInfoParts = dllInfo.split(sep=" ")
    dllName = dllInfoParts[0]
    dllPath = dllInfoParts[2]
    dllBundlePath = os.path.join(bundleDirPath, dllName)

    if dllPath.startswith("/mingw64/bin"):
        dllPath = os.path.join("D:/msys64", dllPath[1:])
        shutil.copyfile(dllPath, dllBundlePath)
    if dllPath.find("videoplayer") != -1:
      if dllPath.startswith("/d"):
          dllPath = os.path.join("D:", dllPath[2:])
      shutil.copyfile(dllPath, dllBundlePath)

shutil.copyfile("videoplayer.exe", "bundle/videoplayer.exe")
shutil.copyfile("song.ttf", "bundle/song.ttf")
shutil.copyfile("config.json", "bundle/config.json")

# usage
# ldd videoplayer.exe | python bundle.py