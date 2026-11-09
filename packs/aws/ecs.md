# ECS
List and inspect ECS clusters, services and tasks, shell into a task, redeploy a service or stop a task

## List clusters
Lists the ECS clusters in this account and region.

```sh
aws ecs list-clusters
```

## List services
Lists the services running in an ECS cluster.

```sh
aws ecs list-services --cluster <cluster>
```

- `<cluster>` Which cluster

## List tasks
Lists the running tasks of an ECS service.

```sh
aws ecs list-tasks --cluster <cluster> --service-name <service>
```

- `<cluster>` Which cluster
- `<service>` Which service

## Describe tasks
Shows a task's status, containers, health and why it stopped, if it did.

```sh
aws ecs describe-tasks --cluster <cluster> --tasks "<task>"
```

- `<cluster>` Which cluster
- `<task>` Which task

## Describe a service
Shows a service's task counts, deployments and recent events.

```sh
aws ecs describe-services --cluster <cluster> --services <service>
```

- `<cluster>` Which cluster
- `<service>` Which service

## Shell into a task
> [!CAUTION]
> Opens a shell inside a live container, where anything you run affects that service. It needs ECS Exec turned on for the service.

```sh needs=session-manager-plugin
aws ecs execute-command --cluster <cluster> --task "<task>" --container <container> --interactive --command /bin/sh
```

- `<cluster>` Which cluster
- `<task>` Which task
- `<container>` Which container

## Redeploy a service
> [!CAUTION]
> Replaces every task of a live service with new ones, restarting it.

```sh
aws ecs update-service --cluster <cluster> --service <service> --force-new-deployment
```

- `<cluster>` Which cluster
- `<service>` Which service

## Stop a task
> [!CAUTION]
> Stops a running task straight away. On a live service, that can drop what it's serving.

```sh
aws ecs stop-task --cluster <cluster> --task "<task>"
```

- `<cluster>` Which cluster
- `<task>` Which task
