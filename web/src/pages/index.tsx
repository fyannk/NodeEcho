import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';

const points = [
  {
    title: 'No Node access for your operators',
    body: 'An operator scoped to its namespace still needs to know when its pods sit on a cordoned or tainted node, or in which zone. NodeEcho writes that onto the pods, so the operator reads its own pods and nothing else.',
  },
  {
    title: 'A plain echo, no interpretation',
    body: 'Cordon, the taints you list, the labels and annotations you list: copied as they are, following later Node changes. NodeEcho never decides that a node is draining; each consumer keeps its own policy.',
  },
  {
    title: 'A key only when it says something',
    body: 'unschedulable is "true" or absent; the taints annotation is absent when no listed taint is there. Hundreds of healthy pods carry nothing, and a healthy pod is never written.',
  },
  {
    title: 'Never another writer’s key, never more than its own',
    body: 'Server-side apply, one field manager per rule: a value from the pod template is never overwritten. Two admission policies keep others from forging the keys, and NodeEcho itself from touching anything but them.',
  },
];

export default function Home(): React.ReactElement {
  return (
    <Layout
      title="Node facts, echoed onto pods"
      description="A Kubernetes controller that echoes Node facts onto the pods running there, so namespace-scoped operators need no access to Nodes.">
      <main className="ne-page">
        <header>
          <p className="ne-eyebrow">
            <img
              className="ne-eyebrow__mark"
              src={useBaseUrl('img/mark.svg')}
              alt=""
              width={84}
              height={63}
            />
            <a className="fy-wordmark" href="https://github.com/fyannk">
              fyannk
            </a>
            <a className="ne-eyebrow__repo" href="https://github.com/fyannk/NodeEcho">
              fyannk/NodeEcho
            </a>
          </p>
          <h1 className="ne-title">Node facts, echoed onto pods</h1>
          <p className="ne-lede">
            NodeEcho is a Kubernetes controller. It watches Nodes and writes
            what you ask for — <code>spec.unschedulable</code>, selected
            taints, selected labels and annotations — onto the pods running
            there, as pod labels and annotations. CloudNativePG is the first
            consumer in view; the contract is generic.
          </p>
          <p className="ne-independent">
            An independent, personal project, not affiliated with, endorsed by
            or sponsored by the Kubernetes project, the CloudNativePG project,
            or any company.
          </p>
          <div className="ne-actions">
            <Link className="button button--primary" to="/docs/tutorials/getting-started/">
              Get started
            </Link>
            <Link className="button button--outline" to="/docs/">
              What it is
            </Link>
            <Link className="button button--outline" to="/docs/reference/contract/">
              Contract v1
            </Link>
          </div>
        </header>

        <section className="ne-section" aria-labelledby="why-title">
          <h2 id="why-title">What it gives you</h2>
          <ul className="ne-points">
            {points.map((point) => (
              <li key={point.title}>
                <strong>{point.title}</strong>
                <span>{point.body}</span>
              </li>
            ))}
          </ul>
        </section>

        <section className="ne-section" aria-labelledby="status-title">
          <h2 id="status-title">Project status</h2>
          <p>
            <span className="ne-status ne-status--warn">▲ Pre-release</span>
          </p>
          <p>
            Contract v1 is implemented and tested on a real API server and on
            k3d clusters, from Kubernetes 1.34 to 1.36. There is no released
            image yet, and no consumer reads the keys upstream: see{' '}
            <Link to="/docs/overview/status/">what is missing before production</Link>.
          </p>
        </section>
      </main>
    </Layout>
  );
}
