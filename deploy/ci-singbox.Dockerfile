FROM busybox:1.37.0
RUN mkdir -p /usr/local/bin && cp /bin/true /usr/local/bin/sing-box
