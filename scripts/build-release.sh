#!/usr/bin/env bash
set -euo pipefail

version="${1:-0.1.0}"
version="${version#v}"
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${project_root}/dist"
work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT

mkdir -p "${dist_dir}" "${work_dir}/rpmbuild/BUILD" "${work_dir}/rpmbuild/BUILDROOT" "${work_dir}/rpmbuild/RPMS" "${work_dir}/rpmbuild/SOURCES" "${work_dir}/rpmbuild/SPECS" "${work_dir}/rpmbuild/SRPMS"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=v${version}" \
  -o "${work_dir}/slurm-insights-exporter" \
  "${project_root}/cmd/slurm-insights-exporter"

archive="slurm-insights-exporter_${version}_linux_x86_64"
mkdir -p "${work_dir}/${archive}"
install -m0755 "${work_dir}/slurm-insights-exporter" "${work_dir}/${archive}/"
install -m0644 "${project_root}/README.md" "${project_root}/LICENSE" "${work_dir}/${archive}/"
install -m0644 "${project_root}/deploy/systemd/slurm-insights-exporter.service" "${project_root}/deploy/systemd/slurm-insights-exporter.sysconfig" "${work_dir}/${archive}/"
tar -C "${work_dir}" -czf "${dist_dir}/${archive}.tar.gz" "${archive}"

dashboard_archive="slurm-insights-exporter_${version}_grafana_dashboards"
mkdir -p "${work_dir}/${dashboard_archive}/grafana" "${work_dir}/${dashboard_archive}/prometheus" "${work_dir}/${dashboard_archive}/monitoring"
cp -R "${project_root}/deploy/grafana/dashboards" "${project_root}/deploy/grafana/provisioning" "${work_dir}/${dashboard_archive}/grafana/"
install -m0644 "${project_root}/deploy/grafana/README.md" "${work_dir}/${dashboard_archive}/grafana/"
install -m0644 "${project_root}/deploy/prometheus-rules.yaml" "${work_dir}/${dashboard_archive}/prometheus/"
install -m0644 "${project_root}/deploy/monitoring/prometheus.yml" "${project_root}/deploy/monitoring/docker-compose.yml" "${work_dir}/${dashboard_archive}/monitoring/"
tar -C "${work_dir}" -czf "${dist_dir}/${dashboard_archive}.tar.gz" "${dashboard_archive}"

install -m0755 "${work_dir}/slurm-insights-exporter" "${work_dir}/rpmbuild/SOURCES/"
install -m0644 "${project_root}/deploy/systemd/slurm-insights-exporter.service" "${work_dir}/rpmbuild/SOURCES/"
install -m0644 "${project_root}/deploy/systemd/slurm-insights-exporter.sysconfig" "${work_dir}/rpmbuild/SOURCES/"
install -m0644 "${project_root}/LICENSE" "${work_dir}/rpmbuild/SOURCES/"
install -m0644 "${project_root}/packaging/rpm/slurm-insights-exporter.spec" "${work_dir}/rpmbuild/SPECS/"

for rpm_dist in .el8 .el9; do
  rpmbuild -bb \
    --define "_topdir ${work_dir}/rpmbuild" \
    --define "_tmppath ${work_dir}" \
    --define "version ${version}" \
    --define "dist ${rpm_dist}" \
    "${work_dir}/rpmbuild/SPECS/slurm-insights-exporter.spec"
done
find "${work_dir}/rpmbuild/RPMS" -type f -name '*.rpm' -exec cp {} "${dist_dir}/" \;

(cd "${dist_dir}" && sha256sum "${archive}.tar.gz" "${dashboard_archive}.tar.gz" slurm-insights-exporter-"${version}"-1.el8.x86_64.rpm slurm-insights-exporter-"${version}"-1.el9.x86_64.rpm > checksums.txt)
echo "Release artifacts written to ${dist_dir}"
