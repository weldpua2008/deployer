package libvirt_kvm

var TmpltCpuConfig = `<cpu mode='custom' match='exact'>
    <model fallback='allow'>Westmere</model>
    {{.CPUPolicy}}
    {{.NUMAConfig}}
  </cpu>`
