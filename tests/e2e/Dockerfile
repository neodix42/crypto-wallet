ARG BASE=ubuntu:24.04
FROM ${BASE}
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y libgtk-3-0t64 libseccomp2 libnss3 libasound2t64 libgbm1 libxss1 libxtst6 libdrm2 libatspi2.0-0t64 libxcomposite1 libxdamage1 libxrandr2 xvfb dbus-x11 xdotool procps && useradd -m wallet
COPY dist/CryptoWallet-linux-x64 /opt/cryptowallet
COPY tests/e2e/container.sh /usr/local/bin/wallet-package-test
RUN chmod 755 /usr/local/bin/wallet-package-test
USER wallet
ENV DISPLAY=:99
ENTRYPOINT ["/usr/local/bin/wallet-package-test"]
