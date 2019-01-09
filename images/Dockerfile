# cloudfoundry/cflinuxfs2 ; cloudfoundry/cflinuxfs2 ; cloudfoundry/windows2016fs
ARG stack

FROM golang:1.9.6 as lifecycle
ARG diego_version=2.7.0
ARG diego_repo=github.com/cloudfoundry/diego-release
ARG bal_repo=code.cloudfoundry.org/buildpackapplifecycle
WORKDIR /diego
RUN git clone --single-branch "https://${diego_repo}" . && \
  git checkout "v${diego_version}" && \
  git submodule update --init --recursive \
    src/code.cloudfoundry.org/archiver \
    src/code.cloudfoundry.org/buildpackapplifecycle \
    src/code.cloudfoundry.org/bytefmt \
    src/code.cloudfoundry.org/cacheddownloader \
    src/code.cloudfoundry.org/goshims \
    src/code.cloudfoundry.org/lager \
    src/code.cloudfoundry.org/systemcerts \
    src/github.com/cloudfoundry-incubator/credhub-cli \
    src/gopkg.in/yaml.v2
RUN GOPATH=/diego CGO_ENABLED=0 go install -a -installsuffix static "${bal_repo}/..."

FROM cloudfoundry/${stack}
COPY --from=lifecycle /diego/bin /tmp/lifecycle
WORKDIR /home/vcap
# RUN mkdir /home/vcap/app && ln -s /home/vcap/app /app
