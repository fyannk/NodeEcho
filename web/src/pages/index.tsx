import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';

const capabilities = [
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
      title="NodeEcho"
      description="A Kubernetes controller that echoes Node facts onto the pods running there, so namespace-scoped operators need no access to Nodes.">
      <header className="ne-hero">
        <div className="ne-hero__inner">
          <img
            className="ne-hero__mark"
            src={useBaseUrl('img/logo.svg')}
            alt=""
            width={220}
            height={220}
          />
          <div className="ne-hero__copy">
            <h1 className="ne-hero__title">
              Node<span>Echo</span>
            </h1>
            <p className="ne-hero__tagline">Kubernetes node context, reflected onto pods.</p>
            <div className="ne-hero__actions">
              <Link className="button button--primary button--lg" to="/docs/tutorials/getting-started/">
                Get started
              </Link>
              <Link className="button button--outline button--lg ne-hero__ghost" to="/docs/">
                What it is
              </Link>
            </div>
          </div>
        </div>
      </header>

      <main className="ne-main">
        <p className="ne-lede">
          NodeEcho is a Kubernetes controller. It watches Nodes and writes what
          you ask for — <code>spec.unschedulable</code>, selected taints,
          selected labels and annotations — onto the pods running there, as pod
          labels and annotations. CloudNativePG is the first consumer in view;
          the contract is generic.
        </p>

        <div className="ne-grid">
          {capabilities.map((capability) => (
            <section className="ne-card" key={capability.title}>
              <h2>{capability.title}</h2>
              <p>{capability.body}</p>
            </section>
          ))}
        </div>

        <div className="ne-note">
          <p>
            <strong>Status.</strong> Contract v1 is implemented and tested on
            a real API server and on k3d clusters, from Kubernetes 1.34 to 1.36.
            There is no released image yet, and no consumer reads the keys
            upstream: see <Link to="/docs/overview/status/">what is missing
            before production</Link>.
          </p>
        </div>
      </main>
    </Layout>
  );
}
