import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'NodeEcho',
  tagline: 'Kubernetes node context, reflected onto pods',
  favicon: 'img/logo.svg',

  // GitHub Pages for fyannk/NodeEcho. Both are case-sensitive: served
  // under /nodeecho/ every stylesheet and script 404s, which renders as
  // an unstyled page rather than as an error.
  url: 'https://fyannk.github.io',
  baseUrl: '/NodeEcho/',
  trailingSlash: true,

  organizationName: 'fyannk',
  projectName: 'NodeEcho',

  onBrokenLinks: 'throw',

  markdown: {
    // .md pages are plain Markdown, so that the contract's <key> and
    // <prefix> placeholders are text rather than JSX.
    format: 'detect',
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: 'docs',
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/fyannk/NodeEcho/tree/main/web/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],
  themes: [
    '@docusaurus/theme-mermaid',
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        docsDir: ['docs'],
        searchResultLimits: 8,
        searchResultContextMaxLength: 50,
        language: ['en'],
        indexBlog: false,
        indexPages: false,
      },
    ],
  ],
  themeConfig: {
    metadata: [
      {
        name: 'description',
        content:
          'NodeEcho is a Kubernetes controller that echoes Node facts (cordon, taints, labels, annotations) onto the pods running there, so namespace-scoped operators need no access to Nodes.',
      },
    ],
    colorMode: {
      respectPrefersColorScheme: true,
    },
    mermaid: {
      theme: {light: 'neutral', dark: 'dark'},
    },
    navbar: {
      title: 'NodeEcho',
      logo: {
        alt: 'NodeEcho',
        src: 'img/logo.svg',
        // The navbar is navy in both themes, so the mark is the same file.
        // Without srcDark, Docusaurus renders only a light-themed image and
        // the logo disappears entirely in dark mode.
        srcDark: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: 'Documentation',
        },
        {
          href: 'https://github.com/fyannk/NodeEcho',
          position: 'right',
          className: 'navbar-github',
          'aria-label': 'NodeEcho on GitHub',
        },
      ],
    },
    footer: {
      style: 'dark',
      logo: {
        alt: 'NodeEcho',
        src: 'img/logo.svg',
        srcDark: 'img/logo.svg',
        href: 'https://github.com/fyannk/NodeEcho',
        width: 64,
      },
      copyright: `Copyright © ${new Date().getFullYear()} NodeEcho contributors. Apache-2.0 licensed.`,
      links: [
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/fyannk/NodeEcho'},
            {label: 'Contract v1', to: '/docs/reference/contract/'},
          ],
        },
        {
          title: 'Kubernetes',
          items: [
            {
              label: 'Server-side apply',
              href: 'https://kubernetes.io/docs/reference/using-api/server-side-apply/',
            },
            {
              label: 'ValidatingAdmissionPolicy',
              href: 'https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/',
            },
          ],
        },
      ],
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'yaml', 'go', 'json'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
