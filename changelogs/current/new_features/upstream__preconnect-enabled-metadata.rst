Added :ref:`preconnect_enabled_metadata
<envoy_v3_api_field_config.cluster.v3.Cluster.PreconnectPolicy.preconnect_enabled_metadata>` to
:ref:`PreconnectPolicy <envoy_v3_api_msg_config.cluster.v3.Cluster.PreconnectPolicy>`. When set,
preconnect connections are opened only for hosts whose endpoint metadata matches the configured
matcher. Hosts that do not match still receive connections on demand. Skipped preconnects are
counted on ``cluster.<name>.upstream_cx_preconnect_skipped``.
