#!/usr/bin/env bash
name=''
namespace=''
environ='ci'
cluster=''

usage() { echo "Usage: $0 -s secret -n <namespace> [-e <environment>] -c clustername" 1>&2; exit 1; }

while getopts ":s:n:e:c:v" o; do
    case "${o}" in
        s)
            name=${OPTARG}
            ;;
        n)
            namespace=${OPTARG}
            ;;
        e)
            environ=${OPTARG}
            ;;
        c)
            cluster=${OPTARG}
            ;;
        v)
            set -xv
            ;;
        *)
            usage
            ;;
    esac
done
shift $((OPTIND-1))

if [ -z "$name" ]; then
    usage
fi
if [ -z "$namespace" ]; then
    usage
fi

kubectl get secrets -n ${namespace} ${name} -o json | jq '.data | map_values(@base64d)' > secret.json
aws secretsmanager create-secret --name "${environ}/${namespace}/${name}" \
--description "imported from k8s ${namespace}/${name}" \
--tags "[{\"Key\":\"label/secretsmanager\",\"Value\":\"\"},{\"Key\":\"kubernetes.io/cluster/${cluster}\",\"Value\":\"\"}]" \
--secret-string file://secret.json
rm secret.json