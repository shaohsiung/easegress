# Key Components

## Cluster

**etcd 的包装**

- primary 是 etcd 的 server
- secondary 是 etcd 的 client

**监听etcd的变化, 收到变化后再进行相关的操作**

watcher, syncer

**基于etcd的分布式锁**

## Supervisor

**对象的管理**

在系统初始化时会创建所有的systemController, 还有已经配置的bizController

在运行时接收管理员命令来动态的创建或修改bizController

对其它的模块暴露API, 这样其它模块就可以通过Controller的名称获取到Controller的实例

**接口**

Object

Controller

TrafficGate

Pipeline

## SystemController

单例

目前无法配置、代码写死的

ServiceRegistry、TrafficController

## Biz Controller

哪些?

1. Mesh..
2. Ingress..
3. EaseMonitorMetrics
4. FaasContr..
5. AutoCertManager..
6. ConsulServiceRegistry
7. GlobalFilter
8. EtcdServiceRegistry
9. NacosServiceRegistry
10. ...

运行时可动态创建修改删除等 by ctl

Controller 接口

```go
// Controller is the object in category of Controller.
Controller interface {
    Object

    // Init initializes the Object.
    Init(superSpec *Spec)

    // Inherit also initializes the Object.
    // But it needs to handle the lifecycle of the previous generation.
    // So it's own responsibility for the object to inherit and clean the previous generation stuff.
    // The supervisor won't call Close for the previous generation.
    Inherit(superSpec *Spec, previousGeneration Object)
}
```

举例：IngressController

`translator` 将k8s配置翻译成easegress的配置
`k8s` k8s连接相关、访问k8s

## TrafficObject

```go
// TrafficGate is the object in category of TrafficGate.
TrafficGate interface { // httpserver
    TrafficObject
}

// Pipeline is the object in category of Pipeline.
Pipeline interface { // httppipeline
    TrafficObject
}
```