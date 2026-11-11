# Import with <project>/<flag>/<environment>/<ramp-id>. Importing never
# touches the ramp, even if variation or bucket_by in config differ.
terraform import featureflip_rollout_ramp.checkout web/new-checkout/production/0199a8c4-5b1e-7c2d-9f3a-4e6b8d0c1a2f
