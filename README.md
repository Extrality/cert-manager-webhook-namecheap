# cert-manager webhook for Namecheap

# Instructions for use with Let's Encrypt

Thanks to [Addison van den Hoeven](https://github.com/Addyvan), from https://github.com/cert-manager/cert-manager/issues/646

Use helm to deploy this into your `cert-manager` namespace:

``` sh
# Make sure you're in the right context:
# kubectl config use-context mycontext

# cert-manager is by default in the cert-manager context
helm install -n cert-manager namecheap-webhook deploy/cert-manager-webhook-namecheap/
```

Create the cluster issuers:

``` sh
helm install --set email=yourname@example.com -n cert-manager letsencrypt-namecheap-issuer deploy/letsencrypt-namecheap-issuer/
```

Go to namecheap and set up your API key (note that you'll need to whitelist the
public IP of the k8s cluster to use the webhook), and set the secret:

``` yaml
apiVersion: v1
kind: Secret
metadata:
  name: namecheap-credentials
type: Opaque
stringData:
  apiKey: my_api_key_from_namecheap
  apiUser: my_username_from_namecheap
```

Now you can create a certificate in staging for testing:

``` yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: wildcard-cert-stage
  namespace: default
spec:
  secretName: wildcard-cert-stage
  commonName: "*.<domain>"
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt-stage
  dnsNames:
  - "*.<domain>"
```

And now validate that it worked:

``` sh
kubectl get certificates -n default
kubectl describe certificate wildcard-cert-stage
```

And finally, create your production cert, and it'll be ready to use in the
`wildcard-cert-prod` secret.

``` yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: wildcard-cert-prod
  namespace: default
spec:
  secretName: wildcard-cert-prod
  commonName: "*.<domain>"
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt-prod
  dnsNames:
  - "*.<domain>"
```

TODO: add simple nginx example to test that it works

### Running the test suite

#### Steps

1. Create testdata/namecheap/apiKey.yaml and testdata/namecheap/config.json using your credentials.
2. Run `TEST_ZONE_NAME=example.com. make test` . Note that the domain here should be updated to your own
domain name. Also note that this is a full domain name with a `.` at the end.
3. You should see all tests passing.
4. In case the tests fail: set `useSandbox` to false
