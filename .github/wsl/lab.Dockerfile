# The throwaway distribution .github/workflows/wsl.yml stops and starts, built
# by its Linux job and imported into WSL on a Windows runner, never on a PC
# people use: Ubuntu with systemd and Docker, a sign-out hook's program, and a
# service whose stop takes 40 seconds, longer than WSL's own stop gives it.
FROM ubuntu:24.04
# systemd and Docker Engine, as people run them in their own distribution.
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends systemd systemd-sysv dbus docker.io \
 && rm -rf /var/lib/apt/lists/*
# The journal kept on disk, as Ubuntu's own WSL distribution keeps it: the
# last lines of a start that was cut say when it ended, and how.
RUN mkdir -p /var/log/journal
# WSL starts systemd in it; Windows programs run in it, as by default.
RUN printf '[boot]\nsystemd=true\n[interop]\nenabled=true\n' > /etc/wsl.conf
# Docker makes no bridge and no firewall rules: every WSL 2 distribution
# shares one network, and the containers here need none.
RUN mkdir -p /etc/docker \
 && printf '{"bridge": "none", "iptables": false, "ip6tables": false}\n' > /etc/docker/daemon.json \
 && systemctl enable docker.service docker.socket
# The service that takes 40 seconds to stop, started at each start.
COPY lab-slow.service /etc/systemd/system/lab-slow.service
RUN systemctl enable lab-slow.service
# What wsl-spawner stops first: the service, after a program that leaves a
# mark. The two files are the person's own, and stay when wsl-spawner goes.
COPY 10-mark /etc/wsl-spawner/pre-stop.d/10-mark
RUN chmod 755 /etc/wsl-spawner/pre-stop.d/10-mark \
 && printf '# the lab service first\nlab-slow.service\n' > /etc/wsl-spawner/stop-units \
 && mkdir -p /var/lib/lab
# The containers' image is the Alpine root file system the job checked, and
# the script that starts them; no registry is reached from the runner.
COPY alpine.tar.gz containers.sh /opt/lab/
RUN chmod 755 /opt/lab/containers.sh
