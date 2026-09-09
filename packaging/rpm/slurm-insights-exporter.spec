Name:           slurm-insights-exporter
%global _unitdir /usr/lib/systemd/system
Version:        %{!?version:0.1.0}%{?version}
Release:        1%{?dist}
Summary:        Prometheus, BI, and audit exporter for Slurm
License:        MIT
URL:            https://github.com/lunajos/slurm-insights-exporter
Source0:        slurm-insights-exporter
Source1:        slurm-insights-exporter.service
Source2:        slurm-insights-exporter.sysconfig
Source3:        LICENSE
BuildArch:      x86_64
Requires:       systemd
Recommends:     slurm

%description
Slurm Insights Exporter exposes Slurm controller and slurmdbd accounting data
as Prometheus metrics and BI-friendly JSON. It also supports append-only,
hash-chained history for forecasting, troubleshooting, and audit workflows.

%prep

%build

%install
install -Dpm0755 %{SOURCE0} %{buildroot}%{_bindir}/slurm-insights-exporter
install -Dpm0644 %{SOURCE1} %{buildroot}%{_unitdir}/slurm-insights-exporter.service
install -Dpm0640 %{SOURCE2} %{buildroot}%{_sysconfdir}/sysconfig/slurm-insights-exporter
install -Dpm0644 %{SOURCE3} %{buildroot}%{_licensedir}/%{name}/LICENSE

%post
/usr/bin/systemctl daemon-reload >/dev/null 2>&1 || :

%preun
if [ $1 -eq 0 ]; then
  /usr/bin/systemctl --no-reload disable --now slurm-insights-exporter.service >/dev/null 2>&1 || :
fi

%postun
/usr/bin/systemctl daemon-reload >/dev/null 2>&1 || :
if [ $1 -ge 1 ]; then
  /usr/bin/systemctl try-restart slurm-insights-exporter.service >/dev/null 2>&1 || :
fi

%files
%license %{_licensedir}/%{name}/LICENSE
%config(noreplace) %{_sysconfdir}/sysconfig/slurm-insights-exporter
%{_bindir}/slurm-insights-exporter
%{_unitdir}/slurm-insights-exporter.service

%changelog
* Wed Sep 09 2026 Lunajos <noreply@github.com> - 0.1.0-1
- Initial Linux x86_64 release
