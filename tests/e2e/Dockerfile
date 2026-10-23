ARG BASE=ubuntu:24.04
FROM ${BASE}
ENV DEBIAN_FRONTEND=noninteractive
COPY dist/localwallet-development_0.1.0_amd64.deb /tmp/wallet.deb
RUN apt-get update && apt-get install -y /tmp/wallet.deb xvfb dbus-x11 xdotool procps && rm /tmp/wallet.deb && useradd -m wallet
COPY tests/e2e/container.sh /usr/local/bin/wallet-package-test
RUN chmod 755 /usr/local/bin/wallet-package-test
USER wallet
ENV DISPLAY=:99
ENTRYPOINT ["/usr/local/bin/wallet-package-test"]
