FROM debian:stretch-slim as extract
ARG WITH_JMX
COPY datadog-agent*_amd64.deb /
WORKDIR /output

# Get s6-overlay and check gpg signature
ENV S6_VERSION v1.21.2.2
ADD https://github.com/just-containers/s6-overlay/releases/download/${S6_VERSION}/s6-overlay-amd64.tar.gz /output/s6.tgz
ADD https://github.com/just-containers/s6-overlay/releases/download/${S6_VERSION}/s6-overlay-amd64.tar.gz.sig /tmp/s6.tgz.sig
ADD s6-overlay-key.asc /tmp/s6-overlay-key.asc
RUN apt-get update \
 && apt-get install --no-install-recommends -y dirmngr gpg \
 && gpg --import /tmp/s6-overlay-key.asc \
 && gpg --verify /tmp/s6.tgz.sig /output/s6.tgz

# Extract and cleanup:
#   - unused systemd unit
#   - GPL sources for embedded software  # FIXME: move upstream
#   - docs and manpages                  # FIXME: move upstream
#   - static libraries                   # FIXME: move upstream
#   - jmxfetch on nojmx build
RUN dpkg -x /datadog-agent*_amd64.deb . \
 && echo "Cleanup and trimming" \
 && rm -rf usr etc/init lib \
    opt/datadog-agent/sources \
    opt/datadog-agent/embedded/share/doc \
    opt/datadog-agent/embedded/share/man \
 && find opt/datadog-agent/ -iname "*.a" -delete \
 && if [ -z "$WITH_JMX" ]; then rm -rf opt/datadog-agent/bin/agent/dist/jmx; fi \
 && echo "Linking embedded ssl certificates" \
 && ln -s /opt/datadog-agent/embedded/ssl etc/ssl

# Configuration:
#   - default config files
#   - enable docker check by default
#   - enable process agent
COPY datadog*.yaml etc/datadog-agent/
RUN mv etc/datadog-agent/conf.d/docker.d/conf.yaml.example etc/datadog-agent/conf.d/docker.d/conf.yaml.default \
 && sed -i "s/enabled: false/enabled: true/" etc/datadog-agent/conf.d/process_agent.yaml.default

COPY entrypoint.sh .


FROM debian:stretch-slim
LABEL maintainer "Datadog <package@datadoghq.com>"
ARG WITH_JMX
ENV DOCKER_DD_AGENT=yes \
    PATH=/opt/datadog-agent/bin/agent/:/opt/datadog-agent/embedded/bin/:$PATH \
    CURL_CA_BUNDLE=/opt/datadog-agent/embedded/ssl/certs/cacert.pem \
    S6_KEEP_ENV=1 \
    S6_LOGGING=0

# Install openjdk-8-jre-headless on jmx flavor
RUN if [ -n "$WITH_JMX" ]; then apt-get update \
 && mkdir /usr/share/man/man1 \
 && apt-get install --no-install-recommends -y openjdk-8-jre-headless \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*; fi

# Copy agent from extract stage
COPY --from=extract /output/ /

# Extract s6-overlay
# FIXME: copying it from the extract stage does not work
RUN tar xfz s6.tgz && rm s6.tgz

# Service definitions to start the agents
COPY s6-services  /etc/services.d/

# Prepare for running without root
RUN adduser --group dd-agent \
 && adduser --system --no-create-home --disabled-password --ingroup dd-agent dd-agent \
 && chown -R dd-agent:dd-agent /etc/datadog-agent/ \
 && mkdir /var/log/datadog \
 && chown dd-agent:dd-agent /var/log/datadog/

# Expose DogStatsD and trace-agent ports
EXPOSE 8125/udp 8126/tcp

# Extra conf.d and checks.d
VOLUME ["/conf.d", "/checks.d"]

# Placeholder healthcheck, to be improved
HEALTHCHECK --interval=5m --timeout=20s --retries=1 \
  CMD ["/opt/datadog-agent/bin/agent/agent", "status"]

CMD ["/init"]
ENTRYPOINT ["/entrypoint.sh"]
