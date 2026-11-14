import type {PrismTheme} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

// Code in the palette: the accent for what names things, a quieter green for
// literals, muted for comments. Status colors stay out of it: they only ever
// mean status.
function codeTheme(c: {text: string; muted: string; accent: string; literal: string; surface: string}): PrismTheme {
  return {
    plain: {color: c.text, backgroundColor: c.surface},
    styles: [
      {types: ['comment', 'prolog', 'doctype', 'cdata'], style: {color: c.muted, fontStyle: 'italic'}},
      {types: ['punctuation', 'operator'], style: {color: c.muted}},
      {
        types: ['keyword', 'atrule', 'key', 'property', 'tag', 'selector', 'attr-name', 'function', 'builtin'],
        style: {color: c.accent},
      },
      {
        types: ['string', 'char', 'attr-value', 'number', 'boolean', 'constant', 'symbol', 'inserted'],
        style: {color: c.literal},
      },
      {types: ['deleted'], style: {color: c.muted, textDecorationLine: 'line-through'}},
    ],
  };
}
const codeDark = codeTheme({text: '#e6e4d6', muted: '#9ca393', accent: '#b3bf3a', literal: '#9fc25a', surface: '#090c0a'});
const codeLight = codeTheme({text: '#14200f', muted: '#4f5a48', accent: '#4f6b0e', literal: '#2f5a10', surface: '#fbfaf3'});

const config: Config = {
  title: 'NodeEcho',
  tagline: 'Kubernetes node context, reflected onto pods',
  favicon: 'img/favicon.svg',

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
          // The design tokens first: custom.css maps them onto Infima.
          customCss: ['./src/css/tokens.css', './src/css/custom.css'],
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
  headTags: [
    {
      tagName: 'link',
      attributes: {rel: 'icon', href: '/NodeEcho/img/favicon-32.png', sizes: '32x32', type: 'image/png'},
    },
    {
      tagName: 'link',
      attributes: {rel: 'apple-touch-icon', href: '/NodeEcho/img/apple-touch-icon.png'},
    },
  ],
  themeConfig: {
    image: 'img/social-card.png',
    metadata: [
      {
        name: 'description',
        content:
          'NodeEcho is a Kubernetes controller that echoes Node facts (cordon, taints, labels, annotations) onto the pods running there, so namespace-scoped operators need no access to Nodes.',
      },
    ],
    // Dark is the primary theme; a reader whose system asks for light gets
    // the light tokens.
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    mermaid: {
      theme: {light: 'neutral', dark: 'dark'},
    },
    navbar: {
      title: 'NodeEcho',
      logo: {
        alt: 'NodeEcho',
        // The mark reads on dark and on light as it is, so both themes use
        // the same file. Without srcDark, Docusaurus renders only a
        // light-themed image and the logo disappears in dark mode.
        src: 'img/mark.svg',
        srcDark: 'img/mark.svg',
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
      copyright: `<a class="fy-wordmark" href="https://github.com/fyannk">fyannk</a> · An independent, personal project, not affiliated with, endorsed by or sponsored by the Kubernetes project, the CloudNativePG project, or any company. · Copyright © ${new Date().getFullYear()} NodeEcho contributors, Apache-2.0.`,
      links: [
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/fyannk/NodeEcho'},
            {label: 'Contract v1', to: '/docs/reference/contract/'},
            {label: 'Status', to: '/docs/overview/status/'},
          ],
        },
        {
          title: 'Family',
          items: [
            {label: 'fyannk', href: 'https://github.com/fyannk'},
            {label: 'pgToolBox', href: 'https://fyannk.github.io/pgToolBox/'},
            {label: 'postgres images', href: 'https://fyannk.github.io/postgres/'},
            {label: 'pgAdmin images', href: 'https://fyannk.github.io/pgAdmin/'},
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
      theme: codeLight,
      darkTheme: codeDark,
      additionalLanguages: ['bash', 'yaml', 'go', 'json'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
