# 一、相关设置
## 1、pbx_ws.go在项目中的地址：
/internetal/api/websocket

## 2、测试输入
cd cmd 

go run . --endpoint ws://175.27.250.177:8080

或者go run . --endpoint ws://192.168.1.134:8080

## 3、注意事项！！！
每次提交前**在config.go文件中**务必将密钥修改为：

YOUR_TENCENT_SECRET_ID

YOUR_TENCENT_SECRET_KEY

# 二、进度概况
## 7月16日
完成offer的创建与发送，并成功接收到answer

## 7月17日
完成asr和tts的接入，实现了用户语音输入识别成文字，并将其转化为语音输出的功能

## 7月18日
完成测试代码的封装 



