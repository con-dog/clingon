# Compute and logs
Connect kubectl to EKS, list EC2 instances, open an SSM shell and tail CloudWatch logs

## Connect kubectl to EKS
Adds an EKS cluster to your kubeconfig and makes it kubectl's current context.

```sh needs=kubectl
aws eks update-kubeconfig --name <cluster>
```

- `<cluster>` Which cluster

## Running instances
Lists running EC2 instances with their ID, type and Name tag.

```sh
aws ec2 describe-instances --filters Name=instance-state-name,Values=running --query 'Reservations[].Instances[].[InstanceId,InstanceType,Tags[?Key==`Name`]|[0].Value]' --output table
```

## Shell into instance (SSM)
> [!CAUTION]
> Opens a shell on an EC2 instance through SSM: no SSH keys or open ports needed. Anything you run there affects that machine.

```sh needs=session-manager-plugin
aws ssm start-session --target <instance>
```

- `<instance>` Which instance

## Tail log group
Streams a CloudWatch log group live.

```sh
aws logs tail <group> --follow --since <since>
```

- `<group>` Group
- `<since>` Since
  - (x) `10m`
  - ( ) `1h`
  - ( ) `1d`
