## 分布式采集容器手动使用说明
- 解压缩命令行启动subconverter程序，正常启动使用25500端口
- 修改template.env中的数据库连接配置，CONV_HOST地址为内网ip地址，subconverter程序在哪台机器使用哪台机器的ip地址，本机也要使用内网ip
- 重命名或复制template.env为.env文件
- 使用configs.sql中的结构创建表并参考示例数据修改配置信息
- configs表中mongodb连接串的?authSource=admin必须存在，否则不能授权
- 使用build.sh或手动执行镜像编译
```docker build -t awesome .```
- 确认镜像创建成功运行容器
```docker run --rm awesome --id task123 --type search_products --keyword "wireless headphones" --max 1 --min 1 --code US``` 具体传参自拟
- 容器当前在前台执行，可以加-d在后台执行，执行成功最后会打印诸如
```成功将%d条产品数据保存到MongoDB集合%s 16 task1235555 Task result: done```的字样，说明数据成功保存至mongo数据库，collection名称就是传入的任务id名称
- 如果出错请检查相关配置，机场订阅地址在configs表中，每次都会拉取最新订阅
