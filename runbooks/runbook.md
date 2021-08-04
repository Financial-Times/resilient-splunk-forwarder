<!--
    Written in the format prescribed by https://github.com/Financial-Times/runbook.md.
    Any future edits should abide by this format.
-->

# UPP - Resilient Splunk Forwarder

Forwards logs cached in S3 to Splunk

## Code

resilient-splunk-forwarder

## Primary URL

<https://upp-prod-delivery-glb.upp.ft.com/__resilient-splunk-forwarder/>
<https://upp-prod-publish-glb.upp.ft.com/__resilient-splunk-forwarder/>

## Service Tier

Platinum

## Lifecycle Stage

Production

## Delivered By

content

## Known About By

- mihail.mihaylov
- hristo.georgiev
- elitsa.pavlova
- kalin.arsov
- elina.kaneva
- georgi.ivanov

## Host Platform

AWS

## Architecture

The service reads and deletes objects (logs) from S3 and forwards them to the provided Splunk HEC URL. Logs are immediately deleted from S3. Logs are then dispatched to a set of workers that submit the data to the configured Splunk HEC URL. Failed logs are stored again in S3. Failures also cause exponential backoff so that the endopint is not overwhelmed. However, due to having multiple workers, this will not affect logs that are already dispatched.

## Contains Personal Data

No

## Contains Sensitive Data

No

## Dependencies

- splunkcloud

## Failover Architecture Type

ActiveActive

## Failover Process Type

FullyAutomated

## Failback Process Type

FullyAutomated

## Failover Details

The service is deployed in all clusters. The failover guide for the clusters is located here: <https://github.com/Financial-Times/upp-docs/tree/master/failover-guides/>

## Data Recovery Process Type

FullyAutomated

## Data Recovery Details

Data are stored in S3 and are persistent and not dependant on service failire.

## Release Process Type

FullyAutomated

## Rollback Process Type

Manual

## Release Details

The deployment is automated.

## Key Management Process Type

None

## Key Management Details

The service is not using AWS keys but IAM role to access the S3 bucket

## Monitoring

- https://upp-prod-publish-us.upp.ft.com/__health
- https://upp-prod-publish-eu.upp.ft.com/__health
- https://upp-prod-delivery-us.upp.ft.com/__health
- https://upp-prod-delivery-eu.upp.ft.com/__health
- https://pac-prod-eu.upp.ft.com/__health
- https://pac-prod-us.upp.ft.com/__health

## First Line Troubleshooting

https://github.com/Financial-Times/upp-docs/tree/master/guides/ops/first-line-troubleshooting

## Second Line Troubleshooting

Please refer to the https://github.com/Financial-Times/resilient-splunk-forwarder/blob/master/README.md for more troubleshooting information.
