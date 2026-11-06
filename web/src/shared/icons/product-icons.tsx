import type { ComponentType, CSSProperties, ReactNode } from 'react';

type IconComponent = ComponentType<{ className?: string; style?: CSSProperties; 'aria-hidden'?: boolean }>;

const shapes = {
  workspace: (
    <>
      <path d="M4 21V7h9v14M13 11h7v10M2 21h20M6 3h5v4M7 11h3m-3 4h3m6 0h1m-1 3h1" />
    </>
  ),
  cost: (
    <>
      <path d="M4 8V5l12-3v6M4 8h15a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2Z" />
      <path d="M21 12h-6v5h6m-3-2.5h.5" />
    </>
  ),
  tunnel: (
    <>
      <rect x="2" y="2" width="6" height="6" rx="2" />
      <rect x="16" y="16" width="6" height="6" rx="2" />
      <circle cx="18" cy="5" r="3" />
      <circle cx="5" cy="19" r="3" />
      <path d="M8 5h7M5 8v8M8 19h8M18 8v8m-10-8 8 8" />
    </>
  ),
  documentation: (
    <>
      <path d="M12 6c-3-2-6-2-10-1v14c4-1 7-1 10 1 3-2 6-2 10-1V5c-4-1-7-1-10 1Zm0 0v14M5 9h3m-3 4h3m8-4h3m-3 4h3" />
    </>
  ),
  sidebar: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M9 4v16M5.5 8h.5m-.5 4h.5" />
    </>
  ),
  chevronRight: <path d="m9 5 7 7-7 7" />,
  overview: (
    <>
      <rect x="3" y="3" width="7" height="18" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </>
  ),
  quickstart: (
    <>
      <path d="M9 15 5 11c4-5 8-8 15-7 1 7-2 11-7 15l-4-4Zm-2-6H4l-2 5 5-1m8 4-1 5 5-2v-3M6 17l-3 4m5-2-1 3" />
      <circle cx="15" cy="9" r="2" />
    </>
  ),
  agents: (
    <>
      <rect x="4" y="7" width="16" height="13" rx="4" />
      <path d="M12 7V3m-2 0h4M1 12v4m22-4v4M8 12v2m8-2v2m-6 3h4" />
    </>
  ),
  sessions: (
    <>
      <path d="M5 4h14a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-8l-5 3v-3H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2ZM7 9h10M7 13h6" />
    </>
  ),
  deployments: (
    <>
      <path d="m3 8 9-5 9 5v11l-9 4-9-4V8Zm0 0 9 5 9-5m-9 5v10M8 5l9 5" />
    </>
  ),
  environments: (
    <>
      <rect x="3" y="3" width="18" height="8" rx="2" />
      <rect x="3" y="15" width="18" height="6" rx="2" />
      <path d="m7 6 2 1-2 1m5 0h4M7 18h.5m3.5 0h6" />
    </>
  ),
  resources: (
    <>
      <path d="m5 8 7-3 7 3-7 3-7-3Zm0 0L2 5l7-3 3 3 3-3 7 3-3 3M5 8v10l7 4 7-4V8m-7 3v11" />
    </>
  ),
  skills: (
    <>
      <path d="M9 5H5a2 2 0 0 0-2 2v13a1 1 0 0 0 1 1h15a2 2 0 0 0 2-2v-4M3 17h14M7 9v4" />
      <path d="m16 2 1.8 4.2L22 8l-4.2 1.8L16 14l-1.8-4.2L10 8l4.2-1.8L16 2Z" />
    </>
  ),
  files: (
    <>
      <path d="M3 19V6a2 2 0 0 1 2-2h5l3 4h6a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Zm0-7h18" />
    </>
  ),
  memory: (
    <>
      <path d="M10 3C6 2 4 5 5 8c-4 1-4 6-1 8-1 4 3 7 6 5V3Zm4 0c4-1 6 2 5 5 4 1 4 6 1 8 1 4-3 7-6 5V3ZM6 8l4 2m-6 6 6-2m8-6-4 2m6 6-6-2" />
    </>
  ),
  vaults: (
    <>
      <rect x="3" y="3" width="18" height="17" rx="2" />
      <circle cx="12" cy="11.5" r="4" />
      <path d="m12 9 2 2.5-2 2.5-2-2.5L12 9ZM6 20v2m12-2v2M6 7v3" />
    </>
  ),
  mcp: (
    <>
      <path d="M8 4v5m8-5v5M5 9h14v3a7 7 0 0 1-14 0V9Zm7 10v3M10 12l2 2 2-2" />
    </>
  ),
  models: (
    <>
      <rect x="6" y="6" width="12" height="12" rx="2" />
      <path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4m-10-6 3 3-3 3-3-3 3-3Z" />
    </>
  ),
  observability: <path d="M2 13h5l3-8 4 14 3-8h5" />,
  usage: (
    <>
      <path d="M2 21h20M4 17v-5h3v5m4 0V7h3v10m4 0V3h3v14" />
    </>
  ),
  members: (
    <>
      <circle cx="9" cy="7" r="3" />
      <path d="M2 21v-3a5 5 0 0 1 5-5h4a5 5 0 0 1 5 5v3H2Zm15-17a3 3 0 0 1 0 6m2 4a4 4 0 0 1 3 4v3h-3" />
    </>
  ),
  keys: (
    <>
      <circle cx="7" cy="9" r="4" />
      <path d="m10 12 9 9 3-3-2-2-2 2-3-3 2-2-2-2M6 8h.5" />
    </>
  ),
  webhooks: (
    <>
      <circle cx="12" cy="4" r="2" />
      <circle cx="4" cy="19" r="2" />
      <circle cx="20" cy="19" r="2" />
      <path d="M12 6v5M4 17v-6h16v6M12 11l-3 3m3-3 3 3M8 19h8" />
    </>
  ),
  settings: (
    <>
      <path d="m10 2 4 0 .6 3 2 .9 2.8-1 2 3.4-2.2 2v2.4l2.2 2-2 3.4-2.8-1-2 .9L14 21h-4l-.6-3-2-.9-2.8 1-2-3.4 2.2-2v-2.4l-2.2-2 2-3.4 2.8 1 2-.9L10 2Z" />
      <circle cx="12" cy="11.5" r="3" />
    </>
  ),
} satisfies Record<string, ReactNode>;

function createProductIcon(name: keyof typeof shapes): IconComponent {
  return function ProductIcon(props) {
    return (
      <svg
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        xmlns="http://www.w3.org/2000/svg"
        {...props}
      >
        {shapes[name]}
      </svg>
    );
  };
}

export const productIcons = {
  workspace: createProductIcon('workspace'),
  cost: createProductIcon('cost'),
  tunnel: createProductIcon('tunnel'),
  documentation: createProductIcon('documentation'),
  sidebar: createProductIcon('sidebar'),
  chevronRight: createProductIcon('chevronRight'),
  overview: createProductIcon('overview'),
  quickstart: createProductIcon('quickstart'),
  agents: createProductIcon('agents'),
  sessions: createProductIcon('sessions'),
  deployments: createProductIcon('deployments'),
  environments: createProductIcon('environments'),
  resources: createProductIcon('resources'),
  skills: createProductIcon('skills'),
  files: createProductIcon('files'),
  memory: createProductIcon('memory'),
  vaults: createProductIcon('vaults'),
  mcp: createProductIcon('mcp'),
  models: createProductIcon('models'),
  observability: createProductIcon('observability'),
  usage: createProductIcon('usage'),
  members: createProductIcon('members'),
  keys: createProductIcon('keys'),
  webhooks: createProductIcon('webhooks'),
  settings: createProductIcon('settings'),
};
